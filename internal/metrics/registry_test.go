package metrics

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRegistry_AllCollectorsRegistered(t *testing.T) {
	r := NewRegistry()

	r.HeapAllocBytes.Set(123)
	r.GoroutineCount.Set(7)
	r.UptimeSeconds.Add(1)
	r.GCPauseSeconds.Observe(0.01)
	r.FDCount.WithLabelValues("linux").Set(42)
	r.CPEsByState.WithLabelValues("ready").Set(100)
	r.InformsTotal.WithLabelValues("2 PERIODIC", "ok").Inc()
	r.FaultsTotal.WithLabelValues("cwmp", "9002").Inc()
	r.USPRequests.WithLabelValues("GET", "ok").Inc()
	r.USPRespRTT.WithLabelValues("GET").Observe(0.002)
	r.BootstrapsTotal.Inc()
	r.PeriodicTicksTotal.Inc()
	r.FailedSessionsTotal.Inc()
	r.ScheduledEventsFired.Inc()
	r.AutonomousNotifiesTotal.WithLabelValues("ValueChange", "true").Inc()
	r.MQTTConnState.WithLabelValues("tcp://broker:1883", "connected").Set(1)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("scrape returned %d, want 200", rec.Code)
	}

	body, _ := io.ReadAll(rec.Body)
	out := string(body)

	wantSubstrings := []string{
		`cpe_sim_process_heap_alloc_bytes 123`,
		`cpe_sim_process_goroutines 7`,
		`cpe_sim_process_uptime_seconds_total 1`,
		`cpe_sim_process_gc_pause_seconds_count 1`,
		`cpe_sim_process_fd_count{os="linux"} 42`,
		`cpe_sim_fleet_cpes_by_state{lifecycle_state="ready"} 100`,
		`cpe_sim_cwmp_informs_total{event_code="2 PERIODIC",result="ok"} 1`,
		`cpe_sim_rpc_faults_total{code="9002",protocol="cwmp"} 1`,
		`cpe_sim_usp_requests_total{msg_type="GET",result="ok"} 1`,
		`cpe_sim_usp_request_rtt_seconds_count{msg_type="GET"} 1`,
		`cpe_sim_fleet_bootstraps_total 1`,
		`cpe_sim_fleet_periodic_ticks_total 1`,
		`cpe_sim_fleet_failed_sessions_total 1`,
		`cpe_sim_fleet_scheduled_events_fired_total 1`,
		`cpe_sim_usp_autonomous_notifies_total{gated="true",notify_kind="ValueChange"} 1`,
		`cpe_sim_usp_mqtt_connection_state{broker="tcp://broker:1883",state="connected"} 1`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(out, want) {
			t.Errorf("scrape output missing %q\n---\n%s", want, out)
		}
	}
}

func TestRegistry_Handler_Isolated(t *testing.T) {
	a := NewRegistry()
	b := NewRegistry()

	a.BootstrapsTotal.Add(5)
	b.BootstrapsTotal.Add(99)

	for _, tc := range []struct {
		name string
		r    *Registry
		want string
	}{
		{"a", a, `cpe_sim_fleet_bootstraps_total 5`},
		{"b", b, `cpe_sim_fleet_bootstraps_total 99`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tc.r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
			body, _ := io.ReadAll(rec.Body)
			if !strings.Contains(string(body), tc.want) {
				t.Errorf("registry %s scrape missing %q", tc.name, tc.want)
			}
		})
	}
}
