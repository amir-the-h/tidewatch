//go:build integration

package queue

import (
	"testing"
)

// TestRedisQueue_Integration spins up a real Redis container and verifies a
// full Push → Pop round-trip.  Relies on testcontainers-go.
//
// Run with: go test -tags integration ./internal/queue/...
//
// Implementation note: testcontainers-go is not yet in go.mod; add it when
// the integration test suite is wired up in CI.
func TestRedisQueue_Integration(t *testing.T) {
	t.Skip("testcontainers-go not yet in go.mod — add when wiring CI")
}
