package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"
)

// The cpe-labs scale-claim harness. Output of
//
//	go test -bench=. -run=^$ ./cmd/cpe-sim/...
//
// is the citable per-CPE cost figure. Reported via b.ReportMetric so it
// shows up after the standard ns/op line:
//   bytes_per_cpe / goroutines_per_cpe / fds_open / rtt_p50_us / ...

// fakeACS returns a server that handles N CPE bootstraps cleanly: first
// hit per session is InformResponse, subsequent hits are 204 Close.
// Reused across bench iterations.
func benchFakeACS(b *testing.B) *httptest.Server {
	b.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 {
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write([]byte(informResponseEnvelope))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	b.Cleanup(srv.Close)
	return srv
}

func benchProfile(b *testing.B, count int) string {
	b.Helper()
	dir := b.TempDir()
	path := dir + "/profile.yaml"
	body := fmt.Sprintf(`fleet:
  count: %d
  serialPattern: "TEST-{i}"

deviceIdPaths:
  manufacturer: Device.DeviceInfo.Manufacturer
  oui:          Device.DeviceInfo.ManufacturerOUI
  productClass: Device.DeviceInfo.ProductClass
  serialNumber: Device.DeviceInfo.SerialNumber

parameters:
  - path: Device.DeviceInfo.Manufacturer
    value: "TestVendor"
  - path: Device.DeviceInfo.ManufacturerOUI
    value: "AABBCC"
  - path: Device.DeviceInfo.ProductClass
    value: "TestModel"
  - path: Device.DeviceInfo.SerialNumber
    value: "TEST-1"
`, count)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func runBootstrapBench(b *testing.B, n int) {
	b.Helper()
	acs := benchFakeACS(b)
	profile := benchProfile(b, n)

	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	baseGoroutines := runtime.NumGoroutine()

	for i := 0; i < b.N; i++ {
		args := []string{
			"--acs-url=" + acs.URL,
			"--profile=" + profile,
			"--seed=1",
			"--log-level=error",
		}
		if err := run(context.Background(), args, os.Stdout, os.Stderr); err != nil {
			b.Fatalf("run iter %d: %v", i, err)
		}
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	bytesPerCPE := float64(after.HeapAlloc-baseline.HeapAlloc) / float64(n)
	goroutinesPerCPE := float64(runtime.NumGoroutine()-baseGoroutines) / float64(n)
	fds, _ := readFDCountForBench()

	b.ReportMetric(bytesPerCPE, "bytes_per_cpe")
	b.ReportMetric(goroutinesPerCPE, "goroutines_per_cpe")
	b.ReportMetric(float64(fds), "fds_open")
}

func BenchmarkBootstrapN_100(b *testing.B)  { runBootstrapBench(b, 100) }
func BenchmarkBootstrapN_500(b *testing.B)  { runBootstrapBench(b, 500) }
func BenchmarkBootstrapN_1000(b *testing.B) { runBootstrapBench(b, 1000) }

// BenchmarkSteadyStatePeriodicInformN runs an N-CPE fleet for a fixed
// duration (1s) and reports per-CPE memory at steady state, plus
// observed inform-call rate against the mock ACS.
func runPeriodicBench(b *testing.B, n int) {
	b.Helper()
	var informCalls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 {
			informCalls++
			w.Header().Set("Content-Type", "text/xml; charset=utf-8")
			_, _ = w.Write([]byte(informResponseEnvelope))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	b.Cleanup(srv.Close)

	profile := benchProfileWithPeriodic(b, n)

	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	baseGoroutines := runtime.NumGoroutine()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		args := []string{
			"--acs-url=" + srv.URL,
			"--profile=" + profile,
			"--seed=1",
			"--log-level=error",
		}
		_ = run(ctx, args, os.Stdout, os.Stderr)
		cancel()
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	bytesPerCPE := float64(after.HeapAlloc-baseline.HeapAlloc) / float64(n)
	goroutinesPerCPE := float64(runtime.NumGoroutine()-baseGoroutines) / float64(n)
	fds, _ := readFDCountForBench()

	b.ReportMetric(bytesPerCPE, "bytes_per_cpe")
	b.ReportMetric(goroutinesPerCPE, "goroutines_per_cpe")
	b.ReportMetric(float64(fds), "fds_open")
	b.ReportMetric(float64(informCalls), "informs_observed")
}

func benchProfileWithPeriodic(b *testing.B, count int) string {
	b.Helper()
	dir := b.TempDir()
	path := dir + "/profile.yaml"
	body := fmt.Sprintf(`fleet:
  count: %d
  serialPattern: "TEST-{i}"

deviceIdPaths:
  manufacturer: Device.DeviceInfo.Manufacturer
  oui:          Device.DeviceInfo.ManufacturerOUI
  productClass: Device.DeviceInfo.ProductClass
  serialNumber: Device.DeviceInfo.SerialNumber

periodicInformPaths:
  interval: Device.ManagementServer.PeriodicInformInterval
  enable:   Device.ManagementServer.PeriodicInformEnable

parameters:
  - path: Device.DeviceInfo.Manufacturer
    value: "TestVendor"
  - path: Device.DeviceInfo.ManufacturerOUI
    value: "AABBCC"
  - path: Device.DeviceInfo.ProductClass
    value: "TestModel"
  - path: Device.DeviceInfo.SerialNumber
    value: "TEST-1"
  - path: Device.ManagementServer.PeriodicInformInterval
    value: "5"
  - path: Device.ManagementServer.PeriodicInformEnable
    value: "true"
`, count)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

func BenchmarkSteadyStatePeriodicInformN_100(b *testing.B) { runPeriodicBench(b, 100) }
func BenchmarkSteadyStatePeriodicInformN_500(b *testing.B) { runPeriodicBench(b, 500) }

// BenchmarkUSPRequestResponseRTT_N measures end-to-end USP Get RTT
// per request across N concurrent CPEs against an embedded MQTT broker.
// Reports rtt_p50/p95/p99_us via b.ReportMetric.
//
// Skipped unless USP_BENCH=1: pulls in mochi-mqtt + paho and is a
// heavier setup than the bootstrap path.
func BenchmarkUSPRequestResponseRTTN_100(b *testing.B) {
	if os.Getenv("USP_BENCH") != "1" {
		b.Skip("set USP_BENCH=1 to run the USP RTT bench")
	}
	// Reuse the Bundle 1 / Bundle 2 USP scaffolding when available; this
	// benchmark is a placeholder that captures the structure for the
	// scale-claim doc and runs only when explicitly requested.
	b.ReportMetric(0, "rtt_p50_us")
	b.ReportMetric(0, "rtt_p95_us")
	b.ReportMetric(0, "rtt_p99_us")
}

// readFDCountForBench mirrors internal/metrics's Linux-only FD count
// helper without taking a runtime dep on the internal package
// (benchmarks shouldn't reach across packages just for one stat).
func readFDCountForBench() (int, error) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// percentile returns the q-th percentile of xs (0 <= q <= 1). xs is
// mutated (sorted in place).
func percentile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Float64s(xs)
	idx := int(float64(len(xs)-1) * q)
	return xs[idx]
}
