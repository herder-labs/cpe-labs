package clients_test

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/clients"
)

// fakeClock + fakeTimer support deterministic tick advancement in tests.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c    chan time.Time
	when time.Time
	mu   sync.Mutex
	done bool
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(0, 0)} }

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) NewTimer(d time.Duration) clients.Timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &fakeTimer{c: make(chan time.Time, 1), when: f.now.Add(d)}
	f.timers = append(f.timers, t)
	return t
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	now := f.now
	timers := append([]*fakeTimer(nil), f.timers...)
	f.mu.Unlock()
	for _, t := range timers {
		t.mu.Lock()
		if !t.done && !now.Before(t.when) {
			t.done = true
			select {
			case t.c <- now:
			default:
			}
		}
		t.mu.Unlock()
	}
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }
func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasActive := !t.done
	t.done = true
	return wasActive
}
func (t *fakeTimer) Reset(d time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.done = false
	t.when = time.Unix(0, 0).Add(d)
	t.c = make(chan time.Time, 1)
	return false
}

func TestRunnerTicksFabricatorOnInterval(t *testing.T) {
	tree := loadFabricatorTree(t)
	fab, err := clients.New(clients.Options{
		Path: "Device.Hosts.Host", Type: "lan", Target: 3, ChurnRate: 1, Tree: tree,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	clock := newFakeClock()
	r, err := clients.NewRunner(clients.RunnerOptions{
		Clock: clock,
		Entries: []clients.Entry{{
			Fabricator: fab,
			Interval:   1 * time.Second,
			RNG:        rand.New(rand.NewSource(1)),
		}},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		_ = r.Stop(stopCtx)
	}()

	// Profile pre-mat'd 1 row; target is 3, churn rate 1. Two ticks
	// should reach target.
	for i := 0; i < 3; i++ {
		clock.advance(1100 * time.Millisecond)
		time.Sleep(20 * time.Millisecond) // let goroutine process
	}

	if got := instanceCount(t, tree); got < 3 {
		t.Errorf("after 3 ticks, count=%d want >=3", got)
	}
}
