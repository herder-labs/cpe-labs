package clients

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// Clock is the time abstraction the Runner uses; tests inject a fake.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
}

type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) NewTimer(d time.Duration) Timer {
	t := time.NewTimer(d)
	return &realTimer{t: t}
}

type realTimer struct{ t *time.Timer }

func (r *realTimer) C() <-chan time.Time   { return r.t.C }
func (r *realTimer) Stop() bool            { return r.t.Stop() }
func (r *realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

// RealClock is the default production Clock.
func RealClock() Clock { return realClock{} }

// Entry is one fabricator + its tick cadence.
type Entry struct {
	Fabricator *Fabricator
	Interval   time.Duration
	RNG        *rand.Rand // for jitter
}

// Runner owns one goroutine per Entry; each ticks at its configured
// Interval with ±10% jitter.
type Runner struct {
	entries []Entry
	clock   Clock
	logger  *slog.Logger

	mu      sync.Mutex
	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

type RunnerOptions struct {
	Entries []Entry
	Clock   Clock
	Logger  *slog.Logger
}

func NewRunner(opts RunnerOptions) (*Runner, error) {
	if opts.Clock == nil {
		opts.Clock = RealClock()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	for i, e := range opts.Entries {
		if e.Fabricator == nil {
			return nil, errors.New("clients.NewRunner: nil Fabricator in entry")
		}
		if e.Interval <= 0 {
			return nil, errors.New("clients.NewRunner: Interval must be > 0")
		}
		if e.RNG == nil {
			opts.Entries[i].RNG = rand.New(rand.NewSource(time.Now().UnixNano()))
		}
	}
	return &Runner{
		entries: opts.Entries,
		clock:   opts.Clock,
		logger:  opts.Logger,
		done:    make(chan struct{}),
	}, nil
}

func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("clients.Runner: already started")
	}
	r.started = true
	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.mu.Unlock()

	var wg sync.WaitGroup
	for i := range r.entries {
		e := r.entries[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.runOne(runCtx, e)
		}()
	}
	go func() {
		wg.Wait()
		close(r.done)
	}()
	return nil
}

func (r *Runner) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.started || r.cancel == nil {
		r.mu.Unlock()
		return nil
	}
	r.cancel()
	r.mu.Unlock()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) runOne(ctx context.Context, e Entry) {
	timer := r.clock.NewTimer(jittered(e.Interval, e.RNG))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C():
			if err := e.Fabricator.Tick(ctx); err != nil {
				r.logger.Warn("clients fabricator tick failed", "path", e.Fabricator.Path(), "err", err.Error())
			}
			timer.Reset(jittered(e.Interval, e.RNG))
		}
	}
}

func jittered(base time.Duration, rng *rand.Rand) time.Duration {
	// ±10% uniform jitter.
	return time.Duration(float64(base) * (0.9 + 0.2*rng.Float64()))
}
