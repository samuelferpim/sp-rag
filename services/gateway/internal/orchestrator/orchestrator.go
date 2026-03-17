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
	Config *config.Config
	OpenAI *openai.Client
	Authz  *authz.AuthzClient
	Cache  *cache.RedisCache
	Qdrant *qdrant.Client
}

// New creates a QueryOrchestrator with all required dependencies.
func New(cfg *config.Config, openaiClient *openai.Client, authzClient *authz.AuthzClient, redisCache *cache.RedisCache, qdrantClient *qdrant.Client) *QueryOrchestrator {
	return &QueryOrchestrator{
		Config: cfg,
		OpenAI: openaiClient,
		Authz:  authzClient,
		Cache:  redisCache,
		Qdrant: qdrantClient,
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
	EmbedMs  int64 `json:"embed_ms"`
	AuthzMs  int64 `json:"authz_ms"`
	CacheMs  int64 `json:"cache_ms"`
	QdrantMs int64 `json:"qdrant_ms"`
	LLMMs    int64 `json:"llm_ms"`
	TotalMs  int64 `json:"total_ms"`
}

// QueryResult is the final output of the query pipeline.
type QueryResult struct {
	Answer  string   `json:"answer"`
	Sources []Source `json:"sources"`
	Model   string   `json:"model"`
	Cached  bool     `json:"cached"`
	Timing  Timing   `json:"timing"`
}

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
// Phase 1 (parallel): embed query + get user teams from SpiceDB
// Phase 2 (sequential): cache lookup (needs vector + teams from Phase 1)
// Phase 3 (sequential, on cache miss): Qdrant search → SpiceDB verify → LLM → cache write
func (o *QueryOrchestrator) Execute(ctx context.Context, query, userID string, topK int) (*QueryResult, error) {
	totalStart := time.Now()
	var timing Timing

	// ── Phase 1: Parallel — embed + authz ──────────────────────────
	var queryVector []float32
	var userTeams []string

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

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// ── Phase 2: Cache lookup (needs vector + teams) ───────────────
	cacheStart := time.Now()

	// Semantic cache
	if data, similarity, err := o.Cache.GetSemantic(ctx, queryVector, userTeams); err != nil {
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

	// Exact cache fallback
	if data, err := o.Cache.GetExact(ctx, query, userTeams); err == nil && data != nil {
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

	// Qdrant search with permission pre-filter
	qdrantStart := time.Now()
	filter := buildPermissionFilter(userTeams, userID)
	limit := uint64(topK)

	searchResult, err := o.Qdrant.Query(ctx, &qdrant.QueryPoints{
		CollectionName: o.Config.QdrantCollection,
		Query:          qdrant.NewQueryDense(queryVector),
		Limit:          &limit,
		WithPayload:    qdrant.NewWithPayloadInclude("text", "source_file", "file_path", "page", "chunk_index"),
		Filter:         filter,
	})
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

	// LLM call
	llmStart := time.Now()
	messages := rag.BuildPrompt(query, chunks)
	answer, err := rag.CallLLM(ctx, o.OpenAI, o.Config.OpenAIChatModel, messages)
	timing.LLMMs = time.Since(llmStart).Milliseconds()
	if err != nil {
		slog.Error("failed to call LLM", "error", err)
		return nil, &QueryError{500, "failed to generate answer", err}
	}

	timing.TotalMs = time.Since(totalStart).Milliseconds()

	result := &QueryResult{
		Answer:  answer,
		Sources: sources,
		Model:   o.Config.OpenAIChatModel,
		Timing:  timing,
	}

	// Save to caches (best-effort)
	if respBytes, err := json.Marshal(result); err == nil {
		if err := o.Cache.SetExact(ctx, query, userTeams, respBytes); err != nil {
			slog.Warn("failed to set exact cache", "error", err)
		}
		if err := o.Cache.SetSemantic(ctx, queryVector, userTeams, respBytes); err != nil {
			slog.Warn("failed to set semantic cache", "error", err)
		}
	}

	slog.Info("query processed",
		"user_id", userID,
		"chunks_found", len(chunks),
		"model", o.Config.OpenAIChatModel,
		"embed_ms", timing.EmbedMs,
		"authz_ms", timing.AuthzMs,
		"cache_ms", timing.CacheMs,
		"qdrant_ms", timing.QdrantMs,
		"llm_ms", timing.LLMMs,
		"total_ms", timing.TotalMs,
	)

	return result, nil
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

// buildPermissionFilter creates a Qdrant filter that matches documents the user can access.
// Uses OR (Should): team-based permissions OR direct ownership.
func buildPermissionFilter(teams []string, userID string) *qdrant.Filter {
	conditions := make([]*qdrant.Condition, 0, 2)

	if len(teams) > 0 {
		conditions = append(conditions, &qdrant.Condition{
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

	conditions = append(conditions, &qdrant.Condition{
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

	return &qdrant.Filter{
		Should: conditions,
	}
}
