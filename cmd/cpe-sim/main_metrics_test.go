package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunMetricsEndpoint(t *testing.T) {
	var acsCalls int
	acs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		acsCalls++
		switch acsCalls {
		case 1:
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write([]byte(informResponseEnvelope))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer acs.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	args := []string{
		"--acs-url=" + acs.URL,
		"--profile=" + minimalProfile(t),
		"--metrics-bind-addr=" + addr,
		"--log-level=error",
	}
	runErr := make(chan error, 1)
	go func() {
		runErr <- run(ctx, args, os.Stdout, os.Stderr)
	}()

	scrapeURL := "http://" + addr + "/metrics"
	adminURL := "http://" + addr + "/admin/cpes"

	deadline := time.Now().Add(5 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		resp, err := http.Get(scrapeURL)
		if err == nil && resp.StatusCode == 200 {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			body = string(b)
			if strings.Contains(body, "cpe_sim_fleet_bootstraps_total 1") {
				break
			}
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(body, "cpe_sim_fleet_bootstraps_total 1") {
		t.Errorf("/metrics missing bootstraps_total=1 after bootstrap:\n%s", body)
	}
	for _, want := range []string{
		"cpe_sim_process_heap_alloc_bytes ",
		"cpe_sim_process_goroutines ",
		"cpe_sim_cwmp_informs_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing %q", want)
		}
	}

	resp, err := http.Get(adminURL)
	if err != nil {
		t.Fatalf("admin GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("admin GET status %d", resp.StatusCode)
	}
	var rows []map[string]any
	if jerr := json.NewDecoder(resp.Body).Decode(&rows); jerr != nil {
		t.Fatal(jerr)
	}
	if len(rows) != 1 {
		t.Fatalf("/admin/cpes returned %d rows, want 1", len(rows))
	}
	if rows[0]["lifecycle_state"] != "ready" {
		t.Errorf("lifecycle_state = %v, want \"ready\"", rows[0]["lifecycle_state"])
	}

	cancel()
	select {
	case <-runErr:
	case <-time.After(3 * time.Second):
		t.Fatal("run() did not exit after context cancel")
	}
}
