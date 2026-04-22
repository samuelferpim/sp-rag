package legal

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sp-rag-gateway/internal/orchestrator"
)

type mockOrch struct {
	result *orchestrator.QueryResult
	err    error
	calls  int
}

func (m *mockOrch) Execute(context.Context, string, string, string, int) (*orchestrator.QueryResult, error) {
	m.calls++
	return m.result, m.err
}

func TestHandleQuery_GroundedAllCitationsVerified(t *testing.T) {
	orch := &mockOrch{result: &orchestrator.QueryResult{
		Answer:   "Conforme o Art. 150 da Lei 8.666/1993, o limite é...",
		Grounded: true,
		Sources: []orchestrator.Source{{
			FileName: "lei-8666.pdf",
			Entities: map[string][]string{
				"articles": {"150"},
				"laws":     {"8.666/1993"},
			},
		}},
	}}
	svc := NewService(orch, 6, true)

	reply, err := svc.HandleQuery(context.Background(), "escritorio-a", "user-1", "qual o limite do art 150?")

	require.NoError(t, err)
	assert.Equal(t, StatusGrounded, reply.Status)
	assert.Empty(t, reply.UnverifiedCitations)
	assert.Contains(t, reply.Answer, "Art. 150")
	assert.True(t, reply.Grounded)
}

func TestHandleQuery_HallucinatedLawRejectedInStrict(t *testing.T) {
	orch := &mockOrch{result: &orchestrator.QueryResult{
		Answer:   "O Art. 150 da Lei 9999/2099 determina...",
		Grounded: true,
		Sources: []orchestrator.Source{{
			FileName: "lei-8666.pdf",
			Entities: map[string][]string{
				"articles": {"150"},
				"laws":     {"8.666/1993"},
			},
		}},
	}}
	svc := NewService(orch, 6, true)

	reply, err := svc.HandleQuery(context.Background(), "escritorio-a", "user-1", "?")

	require.NoError(t, err)
	assert.Equal(t, StatusUnverified, reply.Status)
	assert.Len(t, reply.UnverifiedCitations, 1)
	assert.Equal(t, "9999/2099", reply.UnverifiedCitations[0].Value)
	// Strict mode rewrites the answer
	assert.NotContains(t, reply.Answer, "9999/2099")
	assert.Contains(t, reply.Answer, "Não foi possível", "fallback expected")
}

func TestHandleQuery_HallucinatedLawReturnedWithFlagInLenient(t *testing.T) {
	orch := &mockOrch{result: &orchestrator.QueryResult{
		Answer:   "O Art. 150 da Lei 9999/2099 determina...",
		Grounded: true,
		Sources: []orchestrator.Source{{
			Entities: map[string][]string{
				"articles": {"150"},
				"laws":     {"8.666/1993"},
			},
		}},
	}}
	svc := NewService(orch, 6, false) // lenient

	reply, err := svc.HandleQuery(context.Background(), "escritorio-a", "user-1", "?")

	require.NoError(t, err)
	assert.Equal(t, StatusUnverified, reply.Status)
	assert.Len(t, reply.UnverifiedCitations, 1)
	// Lenient mode keeps the original answer so the UI can flag it
	assert.Contains(t, reply.Answer, "9999/2099")
}

func TestHandleQuery_UngroundedByOrchestrator(t *testing.T) {
	orch := &mockOrch{result: &orchestrator.QueryResult{
		Answer:   "fabricated",
		Grounded: false,
	}}
	svc := NewService(orch, 6, true)

	reply, err := svc.HandleQuery(context.Background(), "escritorio-a", "user-1", "?")

	require.NoError(t, err)
	assert.Equal(t, StatusUngrounded, reply.Status)
	assert.NotContains(t, reply.Answer, "fabricated")
}

func TestHandleQuery_NoSources(t *testing.T) {
	orch := &mockOrch{result: &orchestrator.QueryResult{
		Answer:   "anything",
		Grounded: true,
		Sources:  nil,
	}}
	svc := NewService(orch, 6, true)

	reply, err := svc.HandleQuery(context.Background(), "escritorio-a", "user-1", "?")

	require.NoError(t, err)
	assert.Equal(t, StatusNoContext, reply.Status)
}

func TestHandleQuery_NoCitationsInAnswerStillGrounded(t *testing.T) {
	// An answer without any Art./Lei/Decreto is legitimately citation-free
	// (e.g. "A base não trata deste tópico"). Must not be rejected.
	orch := &mockOrch{result: &orchestrator.QueryResult{
		Answer:   "A base legal não cobre este tópico específico.",
		Grounded: true,
		Sources:  []orchestrator.Source{{FileName: "x.pdf"}},
	}}
	svc := NewService(orch, 6, true)

	reply, err := svc.HandleQuery(context.Background(), "escritorio-a", "user-1", "?")

	require.NoError(t, err)
	assert.Equal(t, StatusGrounded, reply.Status)
	assert.Empty(t, reply.UnverifiedCitations)
}

func TestHandleQuery_OrchestratorErrorReturnsSafeReply(t *testing.T) {
	orch := &mockOrch{err: errors.New("qdrant unreachable")}
	svc := NewService(orch, 6, true)

	reply, err := svc.HandleQuery(context.Background(), "escritorio-a", "user-1", "?")

	require.Error(t, err)
	require.NotNil(t, reply)
	assert.Equal(t, StatusOrchError, reply.Status)
	assert.NotEmpty(t, reply.Answer)
}

func TestHandleQuery_RequiresTenantUserQuery(t *testing.T) {
	svc := NewService(&mockOrch{}, 6, true)
	_, err := svc.HandleQuery(context.Background(), "", "u", "q")
	assert.Error(t, err)
	_, err = svc.HandleQuery(context.Background(), "t", "", "q")
	assert.Error(t, err)
	_, err = svc.HandleQuery(context.Background(), "t", "u", "  ")
	assert.Error(t, err)
}

func TestNewService_DefaultsTopKTo6(t *testing.T) {
	svc := NewService(&mockOrch{}, 0, true)
	assert.Equal(t, 6, svc.TopK)
}
