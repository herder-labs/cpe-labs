//go:build acceptance

package harness

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	brokerpkg "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// StartBroker brings up an embedded mochi-mqtt broker on a random
// localhost port. Returns the host, port, and a cleanup func. Mirrors
// cmd/cpe-sim's startBrokerForTest (deliberate duplication; if a
// third consumer emerges, factor both into internal/testbroker).
func StartBroker(t *testing.T) (host string, port int, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	hostStr, portStr, _ := net.SplitHostPort(ln.Addr().String())
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatalf("parse port: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	srv := brokerpkg.New(&brokerpkg.Options{
		InlineClient: true,
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatalf("AddHook: %v", err)
	}
	if err := srv.AddListener(listeners.NewTCP(listeners.Config{
		ID:      "tcp-acceptance",
		Address: fmt.Sprintf("%s:%d", hostStr, port),
	})); err != nil {
		t.Fatalf("AddListener: %v", err)
	}
	go func() {
		if err := srv.Serve(); err != nil {
			t.Logf("broker serve: %v", err)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", hostStr, port), 100*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	return hostStr, port, func() {
		if err := srv.Close(); err != nil {
			t.Logf("broker close: %v", err)
		}
	}
}
