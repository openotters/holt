package dial_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/pkg/dial"
)

// failingTransport refuses every request and counts how often its
// idle connections are dropped.
type failingTransport struct {
	idleDrops atomic.Int32
}

func (t *failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("refused")
}

func (t *failingTransport) CloseIdleConnections() { t.idleDrops.Add(1) }

// A pooled keep-alive connection can outlive the endpoint it was good
// for and pin every redial to the wrong peer; Run must drop idle
// connections between attempts so each one dials fresh.
func TestRunDropsPooledConnectionsBetweenAttempts(t *testing.T) {
	t.Parallel()

	transport := &failingTransport{}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := dial.Run(ctx, dial.Options{
		URL:        "ws://127.0.0.1:0", // never reached: the transport refuses
		HTTPClient: &http.Client{Transport: transport},
		Handler:    http.NewServeMux(),
		Logger:     zap.NewNop(),
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() = %v, want ctx deadline", err)
	}

	if got := transport.idleDrops.Load(); got < 1 {
		t.Fatalf("idle connections dropped %d times, want at least once per failed attempt", got)
	}
}
