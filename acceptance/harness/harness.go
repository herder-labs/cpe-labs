//go:build acceptance

// Package harness is the shared support package for cpe-labs
// acceptance scenarios. It owns service bring-up (mock ACS, embedded
// MQTT broker), wire-byte capture, golden compare, normalization, and
// the cpe-sim binary build cache.
//
// Every file in this package carries //go:build acceptance.
package harness

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// informResponse is the static InformResponse envelope the mock ACS
// returns on the first POST. Mirrors cmd/cpe-sim's test fixture so a
// real ACS round-trip is observable.
const informResponse = `<?xml version="1.0"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:cwmp="urn:dslforum-org:cwmp-1-1">
  <soapenv:Header><cwmp:ID soapenv:mustUnderstand="1">42</cwmp:ID></soapenv:Header>
  <soapenv:Body>
    <cwmp:InformResponse><MaxEnvelopes>1</MaxEnvelopes></cwmp:InformResponse>
  </soapenv:Body>
</soapenv:Envelope>`

// Fixture bundles the per-scenario environment the harness sets up.
type Fixture struct {
	ACS         *httptest.Server // mock ACS (CWMP scenarios; also a 204-sink for USP)
	Capture     *WireCapture
	ProfilePath string // absolute path to the acceptance profile dir
	BinPath     string // absolute path to the cpe-sim binary

	// USP-only fields. Zero values for CWMP-only fixtures.
	BrokerHost    string
	BrokerPort    int
	AgentEID      string
	ControllerEID string // defaults to "self::herder" matching the acceptance profiles

	// Lazy USP publisher; set up on first PublishUSP call. Internal.
	pubMu     sync.Mutex
	pubClient pahoClient
}

// StartCWMPAcceptance brings up a mock CWMP ACS that returns
// InformResponse on the first POST that looks like an Inform, and 204
// No Content on every other request. Every POST body is captured.
//
// Use this for scenarios that do not need a USP broker. USP scenarios
// call StartUSPAcceptance instead.
func StartCWMPAcceptance(t *testing.T) *Fixture {
	t.Helper()
	cap := NewWireCapture()

	acs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			cap.AppendCWMPRequest(body)
		}
		if len(body) > 0 && strings.Contains(string(body), "<cwmp:Inform>") {
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write([]byte(informResponse))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(acs.Close)

	return &Fixture{
		ACS:         acs,
		Capture:     cap,
		ProfilePath: acceptanceProfileDir(t),
		BinPath:     SimBinary(t),
	}
}

// LaunchSim runs the cpe-sim binary with the given args (plus the
// fixture's --acs-url and --profile pre-supplied). Blocks until the
// process exits or the deadline elapses. On non-zero exit, fails the
// test with the captured stdout+stderr.
//
// The simulator runs in one-shot mode unless extraArgs include
// --cr-bind-addr or a profile that triggers daemon mode.
func LaunchSim(t *testing.T, fix *Fixture, deadline time.Duration, extraArgs ...string) {
	t.Helper()

	args := append([]string{
		"--profile=" + fix.ProfilePath,
		"--acs-url=" + fix.ACS.URL,
		"--log-level=error",
	}, extraArgs...)

	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	cmd := exec.CommandContext(ctx, fix.BinPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cpe-sim exited non-zero (%v); combined output:\n%s", err, out)
	}
}

// SimBinary returns the path to a built cpe-sim binary. The build is
// memoized via sync.Once: first call builds, subsequent calls return
// the cached path. Build errors are memoized so a failed build fails
// every scenario fast (no flaky retry).
//
// The binary lives under os.MkdirTemp("", "cpe-sim-accept-*"); the OS
// cleans up on shutdown. Built without -race (acceptance is not a
// race-detector gate; make test-race covers that).
func SimBinary(t *testing.T) string {
	t.Helper()
	binOnce.Do(buildSim)
	if binErr != nil {
		t.Fatalf("build cpe-sim: %v", binErr)
	}
	return binPath
}

var (
	binOnce sync.Once
	binPath string
	binErr  error
)

func buildSim() {
	dir, err := os.MkdirTemp("", "cpe-sim-accept-*")
	if err != nil {
		binErr = fmt.Errorf("mkdir: %w", err)
		return
	}
	binPath = filepath.Join(dir, "cpe-sim")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/cpe-sim")
	cmd.Dir = repoRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		binErr = fmt.Errorf("go build cpe-sim: %v\n%s", err, out)
	}
}

// repoRoot walks up from this file's directory until it finds go.mod.
// Used to anchor `go build` regardless of where the user invoked
// `go test` from. Panics on failure; this is harness bootstrap and a
// missing go.mod means the world is broken.
func repoRoot() string {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		panic("acceptance/harness: runtime.Caller failed")
	}
	dir := filepath.Dir(here)
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	panic("acceptance/harness: go.mod not found walking up from " + here)
}

// acceptanceProfileDir returns the absolute path to
// acceptance/profiles/minimal-tr181/. Resolved from repoRoot() so it
// works regardless of where `go test` was invoked.
func acceptanceProfileDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(), "acceptance", "profiles", "minimal-tr181")
}

// AcceptanceProfilePath returns the absolute path to
// acceptance/profiles/<name>/. Use this when a scenario needs a
// non-default profile (e.g. one with periodicInformPaths, CR
// listener, generators, ...). The path is resolved from repoRoot()
// so it works regardless of where `go test` was invoked.
func AcceptanceProfilePath(name string) string {
	return filepath.Join(repoRoot(), "acceptance", "profiles", name)
}
