// Package legal implements the "Guardião Tributário e Jurídico" vertical —
// a search-and-answer service for law firms and accounting offices querying
// DOU, Receita Federal normatives, and tax legislation.
//
// The vertical is built on the same channel-agnostic pattern as portaria:
// a Service wraps the RAG orchestrator and adds legal-specific semantics;
// channels (web chat for lawyers, REST API for third-party integrations)
// are thin adapters on top.
//
// The distinguishing behavior is CITATION VERIFICATION: every Art./Lei/
// Decreto referenced in the LLM's answer is cross-checked against the
// regex-extracted entities in the retrieved chunks (see etl.extract_entities
// and orchestrator.Source.Entities). If the model invents a law number, the
// answer is rejected even if the orchestrator's grounding check passed.
package legal

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"sp-rag-gateway/internal/orchestrator"
)

// Orchestrator subset used by the Service. An interface so tests can mock
// without a real Qdrant/Redis/SpiceDB.
type Orchestrator interface {
	Execute(ctx context.Context, tenantID, query, userID string, topK int) (*orchestrator.QueryResult, error)
}

const (
	StatusGrounded     = "grounded"
	StatusUnverified   = "unverified_citations"
	StatusUngrounded   = "ungrounded"
	StatusNoContext    = "no_context"
	StatusOrchError    = "orchestrator_error"
)

// Reply is the legal-vertical response. It keeps the full orchestrator
// result for transparency (audit trail is a hard requirement in legal work)
// plus the citation verification outcome.
type Reply struct {
	Answer             string                `json:"answer"`
	Status             string                `json:"status"`
	Sources            []orchestrator.Source `json:"sources"`
	Citations          Citations             `json:"citations,omitempty"`
	UnverifiedCitations []Citation           `json:"unverified_citations,omitempty"`
	Cached             bool                  `json:"cached"`
	Grounded           bool                  `json:"grounded"`
	Model              string                `json:"model,omitempty"`
	Timing             orchestrator.Timing   `json:"timing"`
}

// Service is the legal-tech conversation layer.
//
//   - StrictCitation=true rejects any answer that contains an unverified
//     citation (default for production — a hallucinated law cite is a
//     malpractice vector).
//   - StrictCitation=false returns the answer with the unverified list so
//     the UI can warn the user without blocking. Useful for dev/demo.
type Service struct {
	Orch           Orchestrator
	TopK           int
	StrictCitation bool
}

func NewService(orch Orchestrator, topK int, strict bool) *Service {
	if topK <= 0 {
		topK = 6 // legal queries typically need more context than generic RAG
	}
	return &Service{Orch: orch, TopK: topK, StrictCitation: strict}
}

// HandleQuery is the single entry point for every channel.
//
// Contract: on a non-nil error, Reply is still populated with a user-safe
// message so the channel layer has something to render.
func (s *Service) HandleQuery(ctx context.Context, tenantID, userID, query string) (*Reply, error) {
	query = strings.TrimSpace(query)
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if userID == "" {
		return nil, errors.New("user_id is required")
	}
	if query == "" {
		return nil, errors.New("query is required")
	}

	result, err := s.Orch.Execute(ctx, tenantID, query, userID, s.TopK)
	if err != nil {
		slog.Error("legal: orchestrator error",
			"error", err, "tenant_id", tenantID, "user_id", userID)
		return &Reply{
			Answer: "Não foi possível consultar a base legal no momento. Tente novamente.",
			Status: StatusOrchError,
		}, err
	}

	reply := &Reply{
		Answer:   result.Answer,
		Sources:  result.Sources,
		Cached:   result.Cached,
		Grounded: result.Grounded,
		Model:    result.Model,
		Timing:   result.Timing,
	}

	if !result.Grounded {
		reply.Status = StatusUngrounded
		reply.Answer = legalFallback("a resposta não pôde ser confirmada na base legal disponível")
		return reply, nil
	}

	if len(result.Sources) == 0 {
		reply.Status = StatusNoContext
		return reply, nil
	}

	// Citation verification — the legal-tech-defining step.
	cited := ExtractCitations(result.Answer)
	chunkEntities := make([]map[string][]string, 0, len(result.Sources))
	for _, src := range result.Sources {
		if len(src.Entities) > 0 {
			chunkEntities = append(chunkEntities, src.Entities)
		}
	}
	unverified := VerifyCitations(cited, chunkEntities)
	reply.Citations = cited
	reply.UnverifiedCitations = unverified

	if len(unverified) == 0 {
		reply.Status = StatusGrounded
		slog.Info("legal: answer verified",
			"tenant_id", tenantID, "user_id", userID,
			"citations", citationCount(cited),
			"sources", len(result.Sources),
		)
		return reply, nil
	}

	slog.Warn("legal: unverified citations detected",
		"tenant_id", tenantID, "user_id", userID,
		"unverified", unverified, "strict", s.StrictCitation,
	)
	reply.Status = StatusUnverified
	if s.StrictCitation {
		reply.Answer = legalFallback(
			"a resposta referencia normas que não estão confirmadas nos documentos indexados",
		)
	}
	return reply, nil
}

func legalFallback(reason string) string {
	return "Não foi possível fornecer uma resposta confiável: " + reason +
		". Revise a fonte original antes de qualquer decisão."
}

func citationCount(c Citations) int {
	n := 0
	for _, values := range c {
		n += len(values)
	}
	return n
}
