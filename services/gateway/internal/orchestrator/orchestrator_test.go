package orchestrator

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sp-rag-gateway/internal/rag"
)

// ── Mock Evaluator ───────────────────────────────────────────────────────

// mockEvaluator returns the pre-programmed sequence of verdicts, one per
// call. It asserts that the orchestrator never calls more times than we
// have verdicts queued — catches runaway loops.
type mockEvaluator struct {
	verdicts []rag.EvaluationResult
	err      error
	calls    int32
}

func (m *mockEvaluator) Evaluate(_ context.Context, _ string, _ []rag.Chunk, _ string) (*rag.EvaluationResult, error) {
	idx := int(atomic.AddInt32(&m.calls, 1)) - 1
	if m.err != nil {
		return nil, m.err
	}
	if idx >= len(m.verdicts) {
		return nil, errors.New("mockEvaluator: unexpected extra call")
	}
	v := m.verdicts[idx]
	return &v, nil
}

func (m *mockEvaluator) callCount() int {
	return int(atomic.LoadInt32(&m.calls))
}

// mockRetry counts how many times the orchestrator asks for a rewrite, and
// returns a distinct answer each time so assertions can verify the latest draft.
type mockRetry struct {
	count int32
	err   error
}

func (m *mockRetry) fn(_ context.Context, _ string, _ string) (string, error) {
	n := atomic.AddInt32(&m.count, 1)
	if m.err != nil {
		return "", m.err
	}
	return "rewritten draft #" + string(rune('0'+n)), nil
}

func (m *mockRetry) callCount() int {
	return int(atomic.LoadInt32(&m.count))
}

// ── Tests for refineUntilGrounded ────────────────────────────────────────

func TestRefineUntilGrounded_GroundedOnFirstAttempt(t *testing.T) {
	eval := &mockEvaluator{
		verdicts: []rag.EvaluationResult{{IsGrounded: true, Reason: "ok"}},
	}
	retry := &mockRetry{}

	answer, grounded := refineUntilGrounded(
		context.Background(),
		eval,
		retry.fn,
		"q",
		[]rag.Chunk{{Text: "fact"}},
		"initial draft",
		time.Second,
	)

	assert.True(t, grounded)
	assert.Equal(t, "initial draft", answer, "grounded draft should be returned as-is")
	assert.Equal(t, 1, eval.callCount(), "evaluator should run exactly once")
	assert.Equal(t, 0, retry.callCount(), "no rewrite on first-shot success")
}

func TestRefineUntilGrounded_RetryTriggeredAndSucceeds(t *testing.T) {
	// First verdict: not grounded → triggers retry. Second verdict: grounded.
	eval := &mockEvaluator{
		verdicts: []rag.EvaluationResult{
			{IsGrounded: false, Reason: "hallucinated date"},
			{IsGrounded: true, Reason: "now verified"},
		},
	}
	retry := &mockRetry{}

	answer, grounded := refineUntilGrounded(
		context.Background(),
		eval,
		retry.fn,
		"q",
		[]rag.Chunk{{Text: "fact"}},
		"initial draft",
		time.Second,
	)

	assert.True(t, grounded)
	assert.Equal(t, "rewritten draft #1", answer,
		"after successful rewrite, the refined answer is returned")
	assert.Equal(t, 2, eval.callCount(), "evaluator should run twice")
	assert.Equal(t, 1, retry.callCount(), "one rewrite between the two evals")
}

func TestRefineUntilGrounded_AllAttemptsFailReturnsNotGrounded(t *testing.T) {
	// Both evaluator verdicts say not grounded — exhaust maxEvalRetries.
	eval := &mockEvaluator{
		verdicts: []rag.EvaluationResult{
			{IsGrounded: false, Reason: "claim A unsupported"},
			{IsGrounded: false, Reason: "claim B unsupported"},
		},
	}
	retry := &mockRetry{}

	answer, grounded := refineUntilGrounded(
		context.Background(),
		eval,
		retry.fn,
		"q",
		[]rag.Chunk{{Text: "fact"}},
		"initial draft",
		time.Second,
	)

	assert.False(t, grounded, "exhausted retries must return not-grounded")
	assert.Equal(t, "rewritten draft #1", answer,
		"final answer is the most recent rewrite")
	assert.Equal(t, maxEvalRetries, eval.callCount(),
		"evaluator called exactly maxEvalRetries times")
	// On the last iteration we skip the rewrite (caller uses fallback), so only 1 retry.
	assert.Equal(t, maxEvalRetries-1, retry.callCount(),
		"no rewrite after the final rejected verdict")
}

func TestRefineUntilGrounded_EvaluatorErrorShortCircuits(t *testing.T) {
	eval := &mockEvaluator{err: errors.New("judge went down")}
	retry := &mockRetry{}

	answer, grounded := refineUntilGrounded(
		context.Background(),
		eval,
		retry.fn,
		"q",
		nil,
		"initial draft",
		time.Second,
	)

	assert.False(t, grounded, "evaluator error must not silently claim grounding")
	assert.Equal(t, "initial draft", answer)
	assert.Equal(t, 1, eval.callCount())
	assert.Equal(t, 0, retry.callCount())
}

func TestRefineUntilGrounded_RetryErrorShortCircuits(t *testing.T) {
	eval := &mockEvaluator{
		verdicts: []rag.EvaluationResult{{IsGrounded: false, Reason: "x"}},
	}
	retry := &mockRetry{err: errors.New("LLM hung up")}

	answer, grounded := refineUntilGrounded(
		context.Background(),
		eval,
		retry.fn,
		"q",
		nil,
		"initial draft",
		time.Second,
	)

	assert.False(t, grounded)
	// The previous (rejected) draft is kept; caller overrides with fallback.
	assert.Equal(t, "initial draft", answer)
	assert.Equal(t, 1, eval.callCount())
	assert.Equal(t, 1, retry.callCount())
}

// ── Tests for buildPermissionFilter fail-safe ────────────────────────────

func TestBuildPermissionFilter_PanicsOnEmptyTenant(t *testing.T) {
	assert.PanicsWithValue(t,
		"orchestrator.buildPermissionFilter: tenantID is empty — refusing to query Qdrant without tenant filter",
		func() { buildPermissionFilter("", []string{"team"}, "user") },
		"empty tenantID must not silently produce an unscoped filter",
	)
}

func TestBuildPermissionFilter_HasTenantAndAccessMustClauses(t *testing.T) {
	filter := buildPermissionFilter("acme", []string{"finance"}, "alice")
	require.NotNil(t, filter)
	// Two Must clauses: [tenant_id == acme, Filter{Should: [...]}].
	// Defensive check that we didn't accidentally drop the tenant clause during refactors.
	require.Len(t, filter.Must, 2, "filter must have tenant clause + nested should clause")
}

// ── QueryError basics (guards against accidental breakage) ───────────────

func TestQueryError_UnwrapAndMessage(t *testing.T) {
	inner := errors.New("root cause")
	qe := &QueryError{StatusCode: 502, Message: "upstream bad", Err: inner}
	assert.Equal(t, "upstream bad", qe.Error())
	assert.Equal(t, inner, errors.Unwrap(qe))
}
