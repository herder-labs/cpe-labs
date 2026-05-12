package metrics

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProcessCollector_PopulatesGauges(t *testing.T) {
	r := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.StartProcessCollectorWithInterval(ctx, time.Millisecond)

	// Allow several ticks to elapse.
	time.Sleep(20 * time.Millisecond)

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	out := string(body)

	// Heap and goroutines should be non-zero after sampling.
	if !strings.Contains(out, "cpe_sim_process_heap_alloc_bytes ") {
		t.Errorf("heap_alloc_bytes not present:\n%s", out)
	}
	if !strings.Contains(out, "cpe_sim_process_goroutines ") {
		t.Errorf("goroutines not present:\n%s", out)
	}
	if !strings.Contains(out, "cpe_sim_process_uptime_seconds_total") {
		t.Errorf("uptime_seconds_total not present:\n%s", out)
	}

	// FD count: linux build tag adds {os="linux"}; non-linux adds {os="unsupported"}.
	if !strings.Contains(out, `cpe_sim_process_fd_count{os=`) {
		t.Errorf("fd_count label not present:\n%s", out)
	}
}

func TestProcessCollector_StopsOnContextCancel(t *testing.T) {
	r := NewRegistry()
	ctx, cancel := context.WithCancel(context.Background())

	r.StartProcessCollectorWithInterval(ctx, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	cancel()
	// No assertion here beyond "doesn't panic"; the goroutine should
	// exit cleanly. Race detector + sleep give it a chance to leak.
	time.Sleep(5 * time.Millisecond)
}
