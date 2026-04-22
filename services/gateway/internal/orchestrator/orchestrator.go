package orchestrator

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/qdrant/go-client/qdrant"
	openai "github.com/sashabaranov/go-openai"
	"golang.org/x/sync/errgroup"

	"sp-rag-gateway/internal/authz"
	"sp-rag-gateway/internal/cache"
	"sp-rag-gateway/internal/config"
	"sp-rag-gateway/internal/rag"
)

// QueryOrchestrator coordinates the parallel query pipeline.
type QueryOrchestrator struct {
	Config    *config.Config
	OpenAI    *openai.Client
	Authz     *authz.AuthzClient
	Cache     *cache.RedisCache
	Qdrant    *qdrant.Client
	Router    rag.Router
	Evaluator rag.Evaluator
}

// New creates a QueryOrchestrator with all required dependencies.
func New(cfg *config.Config, openaiClient *openai.Client, authzClient *authz.AuthzClient, redisCache *cache.RedisCache, qdrantClient *qdrant.Client, router rag.Router, evaluator rag.Evaluator) *QueryOrchestrator {
	return &QueryOrchestrator{
		Config:    cfg,
		OpenAI:    openaiClient,
		Authz:     authzClient,
		Cache:     redisCache,
		Qdrant:    qdrantClient,
		Router:    router,
		Evaluator: evaluator,
	}
}

// Source represents a document chunk used in the answer.
type Source struct {
	FileName string  `json:"file_name"`
	FilePath string  `json:"file_path"`
	Page     int     `json:"page"`
	Score    float32 `json:"score"`
	Snippet  string  `json:"snippet"`
}

// Timing captures latency of each pipeline stage in milliseconds.
type Timing struct {
	RouterMs int64 `json:"router_ms"`
	EmbedMs  int64 `json:"embed_ms"`
	AuthzMs  int64 `json:"authz_ms"`
	CacheMs  int64 `json:"cache_ms"`
	QdrantMs int64 `json:"qdrant_ms"`
	LLMMs    int64 `json:"llm_ms"`
	EvalMs   int64 `json:"eval_ms"`
	TotalMs  int64 `json:"total_ms"`
}

// QueryResult is the final output of the query pipeline.
type QueryResult struct {
	Answer   string   `json:"answer"`
	Sources  []Source `json:"sources"`
	Model    string   `json:"model"`
	Cached   bool     `json:"cached"`
	Grounded bool     `json:"grounded"`
	Timing   Timing   `json:"timing"`
}

const (
	// maxEvalRetries is the maximum number of evaluation rounds (draft + rewrite).
	maxEvalRetries = 2
	// fallbackAnswer is returned when the answer fails grounding after all retries.
	fallbackAnswer = "Desculpe, mas não encontrei informação suficiente na base de conhecimento para garantir uma resposta precisa."
)

// QueryError carries an HTTP status code for the handler to use.
type QueryError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *QueryError) Error() string { return e.Message }
func (e *QueryError) Unwrap() error { return e.Err }

// Execute runs the full query pipeline with parallel orchestration.
//
// Phase 1 (parallel): embed query + get user teams from SpiceDB + route complexity
// Phase 2 (sequential): cache lookup scoped by tenant (needs vector + teams from Phase 1)
// Phase 3 (sequential, on cache miss): Qdrant search (tenant-filtered) → SpiceDB verify → LLM → cache write
//
// tenantID is mandatory: it is enforced as a Must filter in Qdrant and as a keyspace
// partition in Redis to guarantee absolute data isolation across clients.
func (o *QueryOrchestrator) Execute(ctx context.Context, tenantID, query, userID string, topK int) (*QueryResult, error) {
	if tenantID == "" {
		return nil, &QueryError{400, "tenant_id is required", nil}
	}

	totalStart := time.Now()
	var timing Timing

	// ── Phase 1: Parallel — embed + authz + route ─────────────────
	var queryVector []float32
	var userTeams []string
	var complexity rag.Complexity

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		start := time.Now()
		embResp, err := o.OpenAI.CreateEmbeddings(gCtx, openai.EmbeddingRequestStrings{
			Input: []string{query},
			Model: openai.EmbeddingModel(o.Config.OpenAIEmbeddingModel),
		})
		timing.EmbedMs = time.Since(start).Milliseconds()
		if err != nil {
			slog.Error("failed to embed query", "error", err)
			return &QueryError{502, "failed to process query", err}
		}
		if len(embResp.Data) == 0 {
			return &QueryError{502, "empty embedding response", nil}
		}
		queryVector = embResp.Data[0].Embedding
		return nil
	})

	g.Go(func() error {
		start := time.Now()
		teams, err := o.Authz.GetUserTeams(gCtx, userID)
		timing.AuthzMs = time.Since(start).Milliseconds()
		if err != nil {
			slog.Error("failed to get user teams", "error", err, "user_id", userID)
			return &QueryError{403, "failed to verify user permissions", err}
		}
		userTeams = teams
		slog.Info("user teams resolved", "user_id", userID, "teams", teams)
		return nil
	})

	g.Go(func() error {
		start := time.Now()
		c, err := o.Router.Classify(gCtx, query)
		timing.RouterMs = time.Since(start).Milliseconds()
		if err != nil {
			slog.Warn("router classification error, defaulting to complex", "error", err)
			complexity = rag.ComplexityComplex
			return nil
		}
		complexity = c
		slog.Info("query classified", "complexity", string(c), "router_ms", timing.RouterMs)
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// ── Phase 2: Cache lookup (needs vector + teams) ───────────────
	cacheStart := time.Now()

	// Semantic cache (tenant-scoped)
	if data, similarity, err := o.Cache.GetSemantic(ctx, tenantID, queryVector, userTeams); err != nil {
		slog.Warn("semantic cache lookup error", "error", err)
	} else if data == nil {
		slog.Debug("semantic cache below threshold", "similarity", similarity, "user_id", userID, "threshold", o.Config.RedisSimilarityThreshold)
	} else {
		var result QueryResult
		if err := json.Unmarshal(data, &result); err == nil {
			result.Cached = true
			timing.CacheMs = time.Since(cacheStart).Milliseconds()
			timing.TotalMs = time.Since(totalStart).Milliseconds()
			result.Timing = timing
			slog.Info("cache hit",
				"type", "semantic",
				"similarity", similarity,
				"user_id", userID,
				"total_ms", timing.TotalMs,
			)
			return &result, nil
		}
	}

	// Exact cache fallback (tenant-scoped)
	if data, err := o.Cache.GetExact(ctx, tenantID, query, userTeams); err == nil && data != nil {
		var result QueryResult
		if err := json.Unmarshal(data, &result); err == nil {
			result.Cached = true
			timing.CacheMs = time.Since(cacheStart).Milliseconds()
			timing.TotalMs = time.Since(totalStart).Milliseconds()
			result.Timing = timing
			slog.Info("cache hit",
				"type", "exact",
				"user_id", userID,
				"total_ms", timing.TotalMs,
			)
			return &result, nil
		}
	}

	timing.CacheMs = time.Since(cacheStart).Milliseconds()
	slog.Info("cache miss", "user_id", userID, "cache_ms", timing.CacheMs)

	// ── Phase 3: Qdrant → SpiceDB verify → LLM ────────────────────

	// Qdrant search with tenant isolation + permission pre-filter
	qdrantStart := time.Now()
	filter := buildPermissionFilter(tenantID, userTeams, userID)
	limit := uint64(topK)

	// Strict per-stage timeout: keeps a hung Qdrant from eating the handler budget.
	qdrantCtx, qdrantCancel := context.WithTimeout(ctx, time.Duration(o.Config.QdrantTimeoutSeconds)*time.Second)
	searchResult, err := o.Qdrant.Query(qdrantCtx, &qdrant.QueryPoints{
		CollectionName: o.Config.QdrantCollection,
		Query:          qdrant.NewQueryDense(queryVector),
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayloadInclude("text", "source_file", "file_path", "page", "chunk_index", "tenant_id", "section_title", "entities"),
		Filter:         filter,
	})
	qdrantCancel()
	timing.QdrantMs = time.Since(qdrantStart).Milliseconds()
	if err != nil {
		slog.Error("failed to search qdrant", "error", err)
		return nil, &QueryError{500, "failed to search documents", err}
	}

	if len(searchResult) == 0 {
		timing.TotalMs = time.Since(totalStart).Milliseconds()
		return &QueryResult{
			Answer:  "No relevant documents found for your query.",
			Sources: []Source{},
			Model:   o.Config.OpenAIChatModel,
			Timing:  timing,
		}, nil
	}

	// SpiceDB post-filter (defense-in-depth)
	chunks, sources := o.filterAndExtract(ctx, searchResult, userID)

	if len(chunks) == 0 {
		timing.TotalMs = time.Since(totalStart).Milliseconds()
		return &QueryResult{
			Answer:  "No relevant documents found for your query.",
			Sources: []Source{},
			Model:   o.Config.OpenAIChatModel,
			Timing:  timing,
		}, nil
	}

	// Select model based on query complexity
	model := o.Config.OpenAIChatModel
	if complexity == rag.ComplexitySimple {
		model = o.Config.OpenAIFastModel
	}

	// ── Step 1 (Draft): Generate initial answer ─────────────────────
	llmStart := time.Now()
	draft, err := o.callLLMWithTimeout(ctx, model, rag.BuildPrompt(query, chunks))
	timing.LLMMs = time.Since(llmStart).Milliseconds()
	if err != nil {
		slog.Error("failed to call LLM", "error", err)
		return nil, &QueryError{500, "failed to generate answer", err}
	}

	// ── Step 2 (Evaluation): Self-reflection retry loop ─────────────
	evalStart := time.Now()
	retryFn := func(ctx context.Context, previous, reason string) (string, error) {
		return o.callLLMWithTimeout(ctx, model, rag.BuildRetryPrompt(query, chunks, previous, reason))
	}
	answer, grounded := refineUntilGrounded(ctx, o.Evaluator, retryFn, query, chunks, draft, o.evalTimeout())
	timing.EvalMs = time.Since(evalStart).Milliseconds()

	if !grounded {
		answer = fallbackAnswer
	}

	timing.TotalMs = time.Since(totalStart).Milliseconds()

	result := &QueryResult{
		Answer:   answer,
		Sources:  sources,
		Model:    model,
		Grounded: grounded,
		Timing:   timing,
	}

	// Save to caches only if answer is grounded (best-effort)
	if grounded {
		if respBytes, err := json.Marshal(result); err == nil {
			if err := o.Cache.SetExact(ctx, tenantID, query, userTeams, respBytes); err != nil {
				slog.Warn("failed to set exact cache", "error", err)
			}
			if err := o.Cache.SetSemantic(ctx, tenantID, queryVector, userTeams, respBytes); err != nil {
				slog.Warn("failed to set semantic cache", "error", err)
			}
		}
	}

	slog.Info("query processed",
		"tenant_id", tenantID,
		"user_id", userID,
		"chunks_found", len(chunks),
		"model", model,
		"complexity", string(complexity),
		"grounded", grounded,
		"router_ms", timing.RouterMs,
		"embed_ms", timing.EmbedMs,
		"authz_ms", timing.AuthzMs,
		"cache_ms", timing.CacheMs,
		"qdrant_ms", timing.QdrantMs,
		"llm_ms", timing.LLMMs,
		"eval_ms", timing.EvalMs,
		"total_ms", timing.TotalMs,
	)

	return result, nil
}

// callLLMWithTimeout wraps rag.CallLLM with the per-stage LLM timeout.
func (o *QueryOrchestrator) callLLMWithTimeout(ctx context.Context, model string, messages []openai.ChatCompletionMessage) (string, error) {
	llmCtx, cancel := context.WithTimeout(ctx, time.Duration(o.Config.LLMTimeoutSeconds)*time.Second)
	defer cancel()
	return rag.CallLLM(llmCtx, o.OpenAI, model, messages)
}

func (o *QueryOrchestrator) evalTimeout() time.Duration {
	return time.Duration(o.Config.EvalTimeoutSeconds) * time.Second
}

// refineUntilGrounded runs the LLM-as-Judge self-reflection loop over an
// already-produced initial draft.
//
// On each iteration we ask the Evaluator whether the current answer is grounded
// in `chunks`. Grounded → return immediately. Not grounded → ask the `retry`
// closure for a rewrite that incorporates the evaluator's reason, and loop.
// Any evaluator or retry error short-circuits the loop and returns the latest
// draft with grounded=false (the caller is responsible for swapping in the
// fallback message).
//
// The loop is capped at maxEvalRetries evaluations total. Each evaluator call
// gets its own `evalTimeout` deadline so a stuck judge never dominates the
// handler budget.
//
// This function is intentionally decoupled from OpenAI/Qdrant so the retry
// semantics can be exercised in pure unit tests.
func refineUntilGrounded(
	ctx context.Context,
	eval rag.Evaluator,
	retry func(ctx context.Context, previous, reason string) (string, error),
	query string,
	chunks []rag.Chunk,
	initialDraft string,
	evalTimeout time.Duration,
) (string, bool) {
	answer := initialDraft
	for attempt := range maxEvalRetries {
		evalCtx, cancel := context.WithTimeout(ctx, evalTimeout)
		result, err := eval.Evaluate(evalCtx, query, chunks, answer)
		cancel()
		if err != nil {
			slog.Warn("evaluation call failed, skipping self-reflection",
				"error", err, "attempt", attempt+1)
			return answer, false
		}

		if result.IsGrounded {
			slog.Info("answer grounded", "attempt", attempt+1)
			return answer, true
		}

		slog.Warn("answer not grounded",
			"attempt", attempt+1,
			"max_attempts", maxEvalRetries,
			"reason", result.Reason,
		)

		// Last attempt: don't bother rewriting; caller will use fallback.
		if attempt >= maxEvalRetries-1 {
			return answer, false
		}

		newAnswer, err := retry(ctx, answer, result.Reason)
		if err != nil {
			slog.Warn("retry LLM call failed", "error", err, "attempt", attempt+1)
			return answer, false
		}
		answer = newAnswer
	}
	return answer, false
}

// filterAndExtract verifies each document via SpiceDB and extracts chunks + sources.
func (o *QueryOrchestrator) filterAndExtract(ctx context.Context, results []*qdrant.ScoredPoint, userID string) ([]rag.Chunk, []Source) {
	chunks := make([]rag.Chunk, 0, len(results))
	sources := make([]Source, 0, len(results))
	checkedDocs := make(map[string]bool)

	authorized := 0
	denied := 0

	for _, point := range results {
		payload := point.GetPayload()
		filePath := payload["file_path"].GetStringValue()

		allowed, checked := checkedDocs[filePath]
		if !checked {
			var err error
			allowed, err = o.Authz.CheckDocumentAccess(ctx, userID, filePath)
			if err != nil {
				slog.Warn("spicedb check failed, denying access",
					"error", err, "file_path", filePath, "user_id", userID)
				allowed = false
			}
			checkedDocs[filePath] = allowed
		}

		if !allowed {
			denied++
			continue
		}
		authorized++

		text := payload["text"].GetStringValue()
		sourceFile := payload["source_file"].GetStringValue()
		page := int(payload["page"].GetIntegerValue())

		chunks = append(chunks, rag.Chunk{
			Text:       text,
			SourceFile: sourceFile,
			Page:       page,
		})

		snippet := text
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}

		sources = append(sources, Source{
			FileName: sourceFile,
			FilePath: filePath,
			Page:     page,
			Score:    point.GetScore(),
			Snippet:  snippet,
		})
	}

	slog.Info("permission check complete",
		"user_id", userID,
		"authorized", authorized,
		"denied", denied,
	)

	return chunks, sources
}

// buildPermissionFilter creates a Qdrant filter that:
//   1. MUST match the tenant_id (hard isolation, zero cross-tenant leakage), AND
//   2. MUST match at least one of the user's access paths (team permissions OR direct ownership).
//
// The inner Should-filter is wrapped as a nested Must condition so the outer AND semantics hold:
//     tenant_id == X  AND  (permission IN teams  OR  uploaded_by == user)
func buildPermissionFilter(tenantID string, teams []string, userID string) *qdrant.Filter {
	// FAIL-SAFE: an empty tenantID here means the Execute() validation was
	// skipped or a new caller forgot to pass it. Panicking prevents issuing
	// a Qdrant query without tenant isolation — a silent data-leak bug.
	if tenantID == "" {
		panic("orchestrator.buildPermissionFilter: tenantID is empty — refusing to query Qdrant without tenant filter")
	}

	// Inner OR: user's access paths.
	orConditions := make([]*qdrant.Condition, 0, 2)

	if len(teams) > 0 {
		orConditions = append(orConditions, &qdrant.Condition{
			ConditionOneOf: &qdrant.Condition_Field{
				Field: &qdrant.FieldCondition{
					Key: "permissions",
					Match: &qdrant.Match{
						MatchValue: &qdrant.Match_Keywords{
							Keywords: &qdrant.RepeatedStrings{
								Strings: teams,
							},
						},
					},
				},
			},
		})
	}

	orConditions = append(orConditions, &qdrant.Condition{
		ConditionOneOf: &qdrant.Condition_Field{
			Field: &qdrant.FieldCondition{
				Key: "uploaded_by",
				Match: &qdrant.Match{
					MatchValue: &qdrant.Match_Keyword{
						Keyword: userID,
					},
				},
			},
		},
	})

	// Outer AND: tenant_id match (MUST) + access-path match (MUST, nested).
	return &qdrant.Filter{
		Must: []*qdrant.Condition{
			{
				ConditionOneOf: &qdrant.Condition_Field{
					Field: &qdrant.FieldCondition{
						Key: "tenant_id",
						Match: &qdrant.Match{
							MatchValue: &qdrant.Match_Keyword{
								Keyword: tenantID,
							},
						},
					},
				},
			},
			{
				ConditionOneOf: &qdrant.Condition_Filter{
					Filter: &qdrant.Filter{Should: orConditions},
				},
			},
		},
	}
}
