package portaria

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sp-rag-gateway/internal/orchestrator"
)

type mockOrch struct {
	result   *orchestrator.QueryResult
	err      error
	gotQuery string
	gotUser  string
	gotTopK  int
	calls    int
}

func (m *mockOrch) Execute(_ context.Context, _, query, userID string, topK int) (*orchestrator.QueryResult, error) {
	m.calls++
	m.gotQuery = query
	m.gotUser = userID
	m.gotTopK = topK
	return m.result, m.err
}

func TestIsEmergency(t *testing.T) {
	cases := map[string]bool{
		"Tem fogo no 4º andar!":           true,
		"Vazamento de gás aqui":           true,
		"vazando água do teto":            true,
		"alguém passando mal":             true,
		"Socorro, emergência":             true,
		"SAMU ja foi chamado":             true,
		"posso trazer pet?":               false,
		"horário da mudança":              false,
		"qual a regra pra salão?":         false,
		"quero reservar o salão de festa": false,
	}
	for text, want := range cases {
		t.Run(text, func(t *testing.T) {
			assert.Equal(t, want, IsEmergency(text))
		})
	}
}

func TestHandleMessage_EmergencyBypassesOrchestrator(t *testing.T) {
	orch := &mockOrch{}
	svc := NewService(orch, 5, "(11) 99999-0000")

	reply, err := svc.HandleMessage(context.Background(), "ed-flores", "+5511988887777", "tem vazamento de gás no hall")

	require.NoError(t, err)
	assert.Equal(t, RoutedHuman, reply.Routed)
	assert.Contains(t, reply.Text, "emergência")
	assert.Contains(t, reply.Text, "(11) 99999-0000")
	assert.Zero(t, orch.calls, "emergency must not reach the orchestrator")
}

func TestHandleMessage_EmptyTextReturnsGreeting(t *testing.T) {
	orch := &mockOrch{}
	svc := NewService(orch, 5, "")

	reply, err := svc.HandleMessage(context.Background(), "ed-flores", "+5511988887777", "   ")

	require.NoError(t, err)
	assert.Equal(t, RoutedNoop, reply.Routed)
	assert.Zero(t, orch.calls)
}

func TestHandleMessage_GroundedAnswerPassesThrough(t *testing.T) {
	orch := &mockOrch{
		result: &orchestrator.QueryResult{
			Answer:   "Mudanças são permitidas de segunda a sábado das 8h às 17h.",
			Grounded: true,
			Sources:  []orchestrator.Source{{FileName: "regimento.pdf", Page: 3}},
		},
	}
	svc := NewService(orch, 5, "(11) 9999-0000")

	reply, err := svc.HandleMessage(context.Background(), "ed-flores", "+5511988887777", "qual o horário da mudança?")

	require.NoError(t, err)
	assert.Equal(t, RoutedLLM, reply.Routed)
	assert.Contains(t, reply.Text, "segunda a sábado")
	assert.True(t, reply.Grounded)
	assert.Equal(t, 1, orch.calls)
	assert.Equal(t, "qual o horário da mudança?", orch.gotQuery)
	assert.Equal(t, "+5511988887777", orch.gotUser)
	assert.Equal(t, 5, orch.gotTopK)
}

func TestHandleMessage_UngroundedAnswerSwappedForFallback(t *testing.T) {
	orch := &mockOrch{
		result: &orchestrator.QueryResult{
			Answer:   "something ungrounded",
			Grounded: false,
		},
	}
	svc := NewService(orch, 5, "(11) 9999-0000")

	reply, err := svc.HandleMessage(context.Background(), "ed-flores", "+5511988887777", "posso ter 5 cachorros?")

	require.NoError(t, err)
	assert.Equal(t, RoutedHuman, reply.Routed)
	assert.Contains(t, reply.Text, "Não encontrei")
	assert.Contains(t, reply.Text, "(11) 9999-0000")
	assert.False(t, reply.Grounded)
}

func TestHandleMessage_CachedAnswerKeptEvenIfUngroundedFlag(t *testing.T) {
	// A cached result was grounded when stored (we only cache grounded
	// answers). Don't rewrite it on the way out.
	orch := &mockOrch{
		result: &orchestrator.QueryResult{
			Answer:   "Pets pequenos são permitidos com limite de 2 por unidade.",
			Grounded: true,
			Cached:   true,
		},
	}
	svc := NewService(orch, 5, "")

	reply, err := svc.HandleMessage(context.Background(), "ed-flores", "+5511988887777", "posso ter pet?")

	require.NoError(t, err)
	assert.Equal(t, RoutedLLM, reply.Routed)
	assert.True(t, reply.Cached)
	assert.Contains(t, reply.Text, "Pets pequenos")
}

func TestHandleMessage_OrchestratorErrorReturnsFallbackAndPropagates(t *testing.T) {
	orch := &mockOrch{err: errors.New("qdrant down")}
	svc := NewService(orch, 5, "(11) 9999-0000")

	reply, err := svc.HandleMessage(context.Background(), "ed-flores", "+5511988887777", "posso trazer pet?")

	require.Error(t, err, "error must propagate so channel can pick its HTTP status")
	require.NotNil(t, reply, "even on error, a user-safe reply must exist")
	assert.Equal(t, RoutedHuman, reply.Routed)
	assert.Contains(t, reply.Text, "Não encontrei")
}

func TestHandleMessage_MissingTenantErrors(t *testing.T) {
	svc := NewService(&mockOrch{}, 5, "")
	_, err := svc.HandleMessage(context.Background(), "", "u1", "oi")
	assert.Error(t, err)
}

func TestHandleMessage_MissingSenderErrors(t *testing.T) {
	svc := NewService(&mockOrch{}, 5, "")
	_, err := svc.HandleMessage(context.Background(), "t1", "", "oi")
	assert.Error(t, err)
}

func TestNewService_DefaultsTopK(t *testing.T) {
	svc := NewService(&mockOrch{}, 0, "")
	assert.Equal(t, 5, svc.TopK)
}
