package authz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAuthzClient_InvalidEndpoint(t *testing.T) {
	// Creating a client with an unreachable endpoint should still succeed
	// (gRPC connections are lazy). The error surfaces on first RPC call.
	client, err := NewAuthzClient("localhost:99999", "test-key")
	require.NoError(t, err)
	assert.NotNil(t, client)
}

func TestCheckDocumentAccess_FailClosed(t *testing.T) {
	// Core security property: if SpiceDB is unreachable, access MUST be denied.
	client, err := NewAuthzClient("localhost:1", "bad-key")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	allowed, err := client.CheckDocumentAccess(ctx, "alice", "documents/secret.pdf")
	assert.Error(t, err, "unreachable SpiceDB should return an error")
	assert.False(t, allowed, "fail-closed: access must be denied when SpiceDB is unavailable")
}

func TestGetUserTeams_FailClosed(t *testing.T) {
	client, err := NewAuthzClient("localhost:1", "bad-key")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	teams, err := client.GetUserTeams(ctx, "alice")
	assert.Error(t, err, "unreachable SpiceDB should return an error")
	assert.Nil(t, teams, "fail-closed: no teams should be returned")
}
