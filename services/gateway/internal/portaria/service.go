// Package portaria implements the "Síndico Virtual" vertical — a chat assistant
// that answers condo residents' questions based on the building's regimento
// interno. The package is intentionally channel-agnostic: WhatsApp and web
// chat are thin adapters under channels/ that call Service.HandleMessage.
package portaria

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"sp-rag-gateway/internal/orchestrator"
)

// Orchestrator is the subset of orchestrator.QueryOrchestrator the Service
// depends on. Declared as an interface so tests can mock it without booting
// Qdrant/Redis/SpiceDB.
type Orchestrator interface {
	Execute(ctx context.Context, tenantID, query, userID string, topK int) (*orchestrator.QueryResult, error)
}

// Routing labels how a given message was handled — useful for metrics and
// conversation analytics downstream.
const (
	RoutedLLM   = "llm"
	RoutedHuman = "human"
	RoutedNoop  = "noop"
)

// Reply is the channel-agnostic response produced by the Service.
// Channels (WhatsApp, web) decide how to render it on the wire.
type Reply struct {
	Text     string                `json:"text"`
	Routed   string                `json:"routed"`
	Cached   bool                  `json:"cached,omitempty"`
	Grounded bool                  `json:"grounded,omitempty"`
	Sources  []orchestrator.Source `json:"sources,omitempty"`
}

// Service coordinates the portaria conversation layer on top of the generic
// RAG orchestrator. It adds two portaria-specific behaviors:
//
//  1. Emergency bypass — if the message mentions a fire, leak, medical
//     emergency, etc., we skip the LLM entirely and route to a human. The LLM
//     is never the right answer for a burst pipe.
//  2. PT-BR fallback copy — when the orchestrator cannot ground an answer, we
//     reply in a friendly tone that matches the channel (WhatsApp voice) rather
//     than the generic "No relevant documents" of /query.
type Service struct {
	Orch    Orchestrator
	TopK    int
	// HumanContact is appended to emergency and ungrounded replies so the
	// resident always has a way out. Typically the condo's phone number.
	HumanContact string
}

// NewService builds a Service with sane defaults.
func NewService(orch Orchestrator, topK int, humanContact string) *Service {
	if topK <= 0 {
		topK = 5
	}
	return &Service{Orch: orch, TopK: topK, HumanContact: humanContact}
}

// HandleMessage is the single entry point for every channel. It must be safe
// to call concurrently and MUST always return a Reply (even on error) so the
// channel layer has something to send back to the user.
func (s *Service) HandleMessage(ctx context.Context, tenantID, senderID, text string) (*Reply, error) {
	text = strings.TrimSpace(text)
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if senderID == "" {
		return nil, errors.New("sender_id is required")
	}
	if text == "" {
		return &Reply{
			Text:   "Oi! Me manda sua dúvida que eu procuro no regimento do prédio.",
			Routed: RoutedNoop,
		}, nil
	}

	if IsEmergency(text) {
		slog.Info("portaria: emergency bypass",
			"tenant_id", tenantID,
			"sender_id", senderID,
		)
		return &Reply{Text: s.emergencyReply(), Routed: RoutedHuman}, nil
	}

	result, err := s.Orch.Execute(ctx, tenantID, text, senderID, s.TopK)
	if err != nil {
		slog.Error("portaria: orchestrator error",
			"error", err,
			"tenant_id", tenantID,
			"sender_id", senderID,
		)
		// Channel gets a user-safe message; the error is surfaced for logging
		// and for the HTTP status code on the web channel.
		return &Reply{Text: s.fallbackReply(), Routed: RoutedHuman}, err
	}

	if !result.Grounded && !result.Cached {
		// Ungrounded answers are NOT cached by the orchestrator, so we can
		// safely rewrite the copy here for a friendlier tone.
		return &Reply{
			Text:     s.fallbackReply(),
			Routed:   RoutedHuman,
			Grounded: false,
			Sources:  result.Sources,
		}, nil
	}

	return &Reply{
		Text:     result.Answer,
		Routed:   RoutedLLM,
		Cached:   result.Cached,
		Grounded: result.Grounded,
		Sources:  result.Sources,
	}, nil
}

func (s *Service) emergencyReply() string {
	base := "Isso parece uma emergência. Vou avisar a portaria agora."
	if s.HumanContact != "" {
		base += " Se for urgente, ligue direto: " + s.HumanContact + "."
	}
	return base
}

func (s *Service) fallbackReply() string {
	base := "Não encontrei isso no regimento do prédio."
	if s.HumanContact != "" {
		base += " Chama a portaria em " + s.HumanContact + " que te ajudam."
	} else {
		base += " Fala com a portaria pra confirmar."
	}
	return base
}

// emergencyRE matches words/phrases that must route to a human immediately.
// Kept intentionally broad — false positives (routing to human when unnecessary)
// are much cheaper than false negatives (LLM advising on a real emergency).
var emergencyRE = regexp.MustCompile(
	`(?i)\b(incendio|incêndio|fogo|fuma[çc]a|vazamento|vazando|enchente|` +
		`alagamento|gas\s+vazando|g[áa]s\s+vazando|passando\s+mal|` +
		`emerg[êe]ncia|socorro|assalto|invas[ãa]o|ladr[ãa]o|` +
		`desmaiou|convuls[ãa]o|ataque\s+card[ií]aco|parto|ambul[âa]ncia|` +
		`bombeiros?|samu|pol[ií]cia|190|192|193)\b`,
)

// IsEmergency returns true when the message contains a keyword that must not
// be handled by the LLM. See emergencyRE for the matched terms.
func IsEmergency(text string) bool {
	return emergencyRE.MatchString(text)
}
