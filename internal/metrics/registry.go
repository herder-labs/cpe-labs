// Package metrics owns the per-process Prometheus registry and the
// per-CPE-aggregated counters cpe-sim emits. Per-CPE identity is NOT
// a metric label (cardinality blowup at fleet scale); use the admin
// HTTP endpoint for per-CPE introspection.
package metrics

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry bundles every metric cpe-sim emits. One per process.
type Registry struct {
	reg *prometheus.Registry

	// Per-process gauges (sampled by ProcessCollector).
	HeapAllocBytes prometheus.Gauge
	GoroutineCount prometheus.Gauge
	FDCount        *prometheus.GaugeVec
	UptimeSeconds  prometheus.Counter
	GCPauseSeconds prometheus.Histogram

	// Per-CPE state, aggregated. No cpe_id label.
	CPEsByState *prometheus.GaugeVec // labels: lifecycle_state

	// CWMP counters.
	InformsTotal *prometheus.CounterVec // labels: event_code, result
	FaultsTotal  *prometheus.CounterVec // labels: protocol, code

	// USP counters.
	USPRequests *prometheus.CounterVec   // labels: msg_type, result
	USPRespRTT  *prometheus.HistogramVec // labels: msg_type

	// Per-CPE state machine.
	BootstrapsTotal         prometheus.Counter
	PeriodicTicksTotal      prometheus.Counter
	FailedSessionsTotal     prometheus.Counter
	ScheduledEventsFired    prometheus.Counter
	AutonomousNotifiesTotal *prometheus.CounterVec // labels: notify_kind, gated

	// MQTT MTP connection state per broker.
	MQTTConnState *prometheus.GaugeVec // labels: broker, state

	// MQTT MTP connect failures (auth + other). Per-CPE labels are not
	// emitted (cardinality blowup at fleet scale) — the EID goes in the
	// WARN log line. reason is bounded by mqtt.ConnectErrorReason.
	MQTTConnectFailures *prometheus.CounterVec // labels: reason

	// Process-collector bookkeeping.
	uptimeMu   sync.Mutex
	uptimeLast float64
}

// NewRegistry constructs a Registry with every metric registered. The
// returned *prometheus.Registry is isolated from the global default;
// tests and multiple cpe-sim instances in one process don't collide.
func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()
	r := &Registry{reg: reg}

	r.HeapAllocBytes = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cpe_sim", Subsystem: "process", Name: "heap_alloc_bytes",
		Help: "Heap memory currently allocated by the Go runtime, in bytes.",
	})
	r.GoroutineCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "cpe_sim", Subsystem: "process", Name: "goroutines",
		Help: "Current goroutine count (runtime.NumGoroutine).",
	})
	r.FDCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cpe_sim", Subsystem: "process", Name: "fd_count",
		Help: "Open file descriptors. On non-Linux platforms emits 0 with os=unsupported.",
	}, []string{"os"})
	r.UptimeSeconds = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "process", Name: "uptime_seconds_total",
		Help: "Process uptime in seconds since startup.",
	})
	r.GCPauseSeconds = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cpe_sim", Subsystem: "process", Name: "gc_pause_seconds",
		Help:    "Per-cycle GC pause duration.",
		Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
	})
	r.CPEsByState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cpe_sim", Subsystem: "fleet", Name: "cpes_by_state",
		Help: "Count of simulated CPEs in each lifecycle state.",
	}, []string{"lifecycle_state"})
	r.InformsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "cwmp", Name: "informs_total",
		Help: "CWMP Inform RPCs emitted, by event code and result.",
	}, []string{"event_code", "result"})
	r.FaultsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "rpc", Name: "faults_total",
		Help: "Protocol fault responses emitted, by protocol and code.",
	}, []string{"protocol", "code"})
	r.USPRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "usp", Name: "requests_total",
		Help: "USP requests handled, by msg_type and result.",
	}, []string{"msg_type", "result"})
	r.USPRespRTT = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cpe_sim", Subsystem: "usp", Name: "request_rtt_seconds",
		Help:    "Per-USP-request handler RTT (decode → wrap → send).",
		Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 30},
	}, []string{"msg_type"})
	r.BootstrapsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "fleet", Name: "bootstraps_total",
		Help: "Total successful first-contact bootstrap sessions.",
	})
	r.PeriodicTicksTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "fleet", Name: "periodic_ticks_total",
		Help: "Total periodic-Inform timer ticks fired.",
	})
	r.FailedSessionsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "fleet", Name: "failed_sessions_total",
		Help: "Total bootstrap or periodic sessions that returned an error.",
	})
	r.ScheduledEventsFired = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "fleet", Name: "scheduled_events_fired_total",
		Help: "Total deferred Reboot / FactoryReset / boot events fired.",
	})
	r.AutonomousNotifiesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "usp", Name: "autonomous_notifies_total",
		Help: "USP autonomous Notify messages emitted by the Subscription evaluator.",
	}, []string{"notify_kind", "gated"})
	r.MQTTConnState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cpe_sim", Subsystem: "usp", Name: "mqtt_connection_state",
		Help: "MQTT broker connection state count per (broker, state).",
	}, []string{"broker", "state"})
	r.MQTTConnectFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cpe_sim", Subsystem: "usp", Name: "mqtt_connect_failures_total",
		Help: "USP MQTT Connect failures, by coarse reason bucket.",
	}, []string{"reason"})

	for _, c := range []prometheus.Collector{
		r.HeapAllocBytes, r.GoroutineCount, r.FDCount, r.UptimeSeconds, r.GCPauseSeconds,
		r.CPEsByState, r.InformsTotal, r.FaultsTotal, r.USPRequests, r.USPRespRTT,
		r.BootstrapsTotal, r.PeriodicTicksTotal, r.FailedSessionsTotal,
		r.ScheduledEventsFired, r.AutonomousNotifiesTotal, r.MQTTConnState,
		r.MQTTConnectFailures,
	} {
		reg.MustRegister(c)
	}

	return r
}

// Handler returns the Prometheus scrape handler for this registry.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{})
}

// PrometheusRegistry exposes the underlying registry for advanced
// callers (e.g. tests asserting collector membership).
func (r *Registry) PrometheusRegistry() *prometheus.Registry { return r.reg }
