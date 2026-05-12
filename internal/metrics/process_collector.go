package metrics

import (
	"context"
	"runtime"
	"time"
)

// DefaultSampleInterval is the cadence at which the process collector
// samples runtime.MemStats and friends.
const DefaultSampleInterval = 5 * time.Second

// StartProcessCollector launches a goroutine that periodically samples
// runtime stats into the registry's per-process gauges. The goroutine
// exits when ctx is cancelled. Sample cadence is DefaultSampleInterval;
// for tests, use StartProcessCollectorWithInterval.
func (r *Registry) StartProcessCollector(ctx context.Context) {
	r.StartProcessCollectorWithInterval(ctx, DefaultSampleInterval)
}

// StartProcessCollectorWithInterval is StartProcessCollector with an
// explicit sample interval (useful for tests).
func (r *Registry) StartProcessCollectorWithInterval(ctx context.Context, interval time.Duration) {
	start := time.Now()
	go func() {
		r.sample(start)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.sample(start)
			}
		}
	}()
}

// sample reads current runtime state into the gauges. Called by the
// collector goroutine; exported only via the goroutine path.
func (r *Registry) sample(start time.Time) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	r.HeapAllocBytes.Set(float64(ms.HeapAlloc))
	r.GoroutineCount.Set(float64(runtime.NumGoroutine()))

	// GC pause: observe each new pause not yet seen. NumGC monotonic.
	// PauseNs is a circular buffer of the last 256 pauses; emit only
	// the most recent one each tick (over-sampling is fine, prom
	// histograms are append-only).
	if ms.NumGC > 0 {
		latest := ms.PauseNs[(ms.NumGC+255)%256]
		r.GCPauseSeconds.Observe(float64(latest) / 1e9)
	}

	r.UptimeSeconds.Add(0)
	r.advanceUptime(start)

	count, label := readFDCount()
	r.FDCount.WithLabelValues(label).Set(float64(count))
}

// advanceUptime keeps UptimeSeconds in sync with wall time. Because
// Counter has no Set, we add the per-tick delta. The collector tracks
// the prior reading via the closure-captured start time.
func (r *Registry) advanceUptime(start time.Time) {
	// Set-via-delta: derive desired total, compute delta from last total.
	desired := time.Since(start).Seconds()
	r.uptimeMu.Lock()
	delta := desired - r.uptimeLast
	if delta > 0 {
		r.UptimeSeconds.Add(delta)
		r.uptimeLast = desired
	}
	r.uptimeMu.Unlock()
}
