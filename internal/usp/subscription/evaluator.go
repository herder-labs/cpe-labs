package subscription

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/herder-labs/cpe-labs/internal/cwmp/scheduler"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	"github.com/herder-labs/cpe-labs/internal/usp/codec"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/mtp"
	"github.com/herder-labs/cpe-labs/internal/usp/notify"
)

const SubscriptionTablePath = "Device.LocalAgent.Subscription"

// NotifType values mirrored from the TR-181 enum.
const (
	NotifEvent             = "Event"
	NotifValueChange       = "ValueChange"
	NotifObjectCreation    = "ObjectCreation"
	NotifObjectDeletion    = "ObjectDeletion"
	NotifPeriodic          = "Periodic"
	NotifOperationComplete = "OperationComplete"
	NotifOnBoardRequest    = "OnBoardRequest"
)

// Evaluator owns the per-CPE Subscription state and dispatches
// autonomous Notifies on trigger.
type Evaluator struct {
	Tree          *paramtree.Tree
	Adapter       mtp.Adapter
	AgentEID      string
	ControllerEID string
	Scheduler     *scheduler.Scheduler
	CPEID         string
	RNG           *rand.Rand
	Logger        *slog.Logger

	mu              sync.RWMutex
	valueChange     map[string][]subscriptionRef
	objectCreation  map[string][]subscriptionRef
	objectDeletion  map[string][]subscriptionRef
	eventIdx        map[string][]subscriptionRef
	periodicCancels map[string]func()
	started         bool

	rescanCh chan struct{}

	// hookOnce ensures the Tree.OnWrite callback is registered
	// exactly once for the evaluator's lifetime. The tree has no
	// unregister API, so Stop/Start cycles otherwise leak callbacks.
	hookOnce sync.Once
	// enabled gates the hook body. Toggled by Start (true) and Stop
	// (false). A disabled hook is a no-op; rescans only happen while
	// the evaluator is running.
	enabled atomic.Bool
	// rebuildCount is incremented at the start of every rebuild for
	// test visibility. Not part of the public contract.
	rebuildCount atomic.Int64
}

type subscriptionRef struct {
	ID            string
	NotifType     string
	ReferenceList []string
	Recipient     string
}

// New returns an evaluator ready to Start.
func New(tree *paramtree.Tree, adapter mtp.Adapter, agentEID, controllerEID string, sched *scheduler.Scheduler, cpeID string, rng *rand.Rand, logger *slog.Logger) *Evaluator {
	if logger == nil {
		logger = slog.Default()
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &Evaluator{
		Tree:            tree,
		Adapter:         adapter,
		AgentEID:        agentEID,
		ControllerEID:   controllerEID,
		Scheduler:       sched,
		CPEID:           cpeID,
		RNG:             rng,
		Logger:          logger,
		valueChange:     map[string][]subscriptionRef{},
		objectCreation:  map[string][]subscriptionRef{},
		objectDeletion:  map[string][]subscriptionRef{},
		eventIdx:        map[string][]subscriptionRef{},
		periodicCancels: map[string]func(){},
		rescanCh:        make(chan struct{}, 1),
	}
}

// Start kicks off the evaluator: initial scan + Tree.OnWrite hook for
// debounced rescans + per-Periodic-Subscription scheduler entries. Safe
// to call before adapter is connected; Notify emission no-ops if
// adapter.Send fails.
func (e *Evaluator) Start(ctx context.Context) error {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return errors.New("evaluator already started")
	}
	e.started = true
	e.mu.Unlock()

	e.enabled.Store(true)
	e.registerTreeHookOnce()

	e.rebuild()

	go e.runRescanLoop(ctx)
	return nil
}

// RebuildCount returns how many times rebuild() has run on this
// evaluator since construction. Test affordance for asserting hook
// activity; not part of the wire-level contract.
func (e *Evaluator) RebuildCount() int64 { return e.rebuildCount.Load() }

// registerTreeHookOnce installs the Tree.OnWrite filter. The callback
// body checks e.enabled before queueing a rescan, so Stop/Start cycles
// flip the gate without leaking extra registrations.
func (e *Evaluator) registerTreeHookOnce() {
	e.hookOnce.Do(func() {
		e.Tree.OnWrite(func(paths []string, _ paramtree.WriteKind) {
			if !e.enabled.Load() {
				return
			}
			for _, p := range paths {
				if strings.HasPrefix(p, SubscriptionTablePath+".") {
					e.queueRescan()
					return
				}
			}
		})
	})
}

// Stop cancels all in-flight Periodic scheduler entries. Safe to call
// even if Start was never invoked.
func (e *Evaluator) Stop(_ context.Context) error {
	e.enabled.Store(false)
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, cancel := range e.periodicCancels {
		if cancel != nil {
			cancel()
		}
	}
	e.periodicCancels = map[string]func(){}
	e.started = false
	return nil
}

func (e *Evaluator) queueRescan() {
	select {
	case e.rescanCh <- struct{}{}:
	default:
	}
}

func (e *Evaluator) runRescanLoop(ctx context.Context) {
	debounce := 50 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.rescanCh:
			time.Sleep(debounce)
			// Drain any additional triggers that arrived during sleep.
			for {
				select {
				case <-e.rescanCh:
					continue
				default:
				}
				break
			}
			e.rebuild()
		}
	}
}

// rebuild walks the Subscription table from the tree and rebuilds the
// trigger indices. Idempotent; safe to call repeatedly.
func (e *Evaluator) rebuild() {
	e.rebuildCount.Add(1)
	subs := e.scanTable()
	vc := map[string][]subscriptionRef{}
	oc := map[string][]subscriptionRef{}
	od := map[string][]subscriptionRef{}
	ev := map[string][]subscriptionRef{}
	for _, s := range subs {
		switch s.NotifType {
		case NotifValueChange:
			for _, p := range s.ReferenceList {
				vc[p] = append(vc[p], s)
			}
		case NotifObjectCreation:
			for _, p := range s.ReferenceList {
				oc[p] = append(oc[p], s)
			}
		case NotifObjectDeletion:
			for _, p := range s.ReferenceList {
				od[p] = append(od[p], s)
			}
		case NotifEvent:
			for _, p := range s.ReferenceList {
				ev[p] = append(ev[p], s)
			}
		case NotifPeriodic:
			// Period scheduling handled by reschedulePeriodic.
		}
	}

	e.mu.Lock()
	e.valueChange = vc
	e.objectCreation = oc
	e.objectDeletion = od
	e.eventIdx = ev
	e.mu.Unlock()

	e.reschedulePeriodic(subs)
}

// scanTable enumerates every Device.LocalAgent.Subscription.{i}. row
// and returns the enabled rows as subscriptionRef structs.
func (e *Evaluator) scanTable() []subscriptionRef {
	out := []subscriptionRef{}
	children, err := e.Tree.Children(SubscriptionTablePath)
	if err != nil {
		return out
	}
	for _, c := range children {
		segName := strings.TrimSuffix(strings.TrimPrefix(c.Name, SubscriptionTablePath+"."), ".")
		if _, perr := strconv.Atoi(segName); perr != nil {
			continue
		}
		row := c.Name
		enabled := e.readLeaf(row + "Enable")
		if enabled != "true" {
			continue
		}
		ref := subscriptionRef{
			ID:            e.readLeaf(row + "ID"),
			NotifType:     e.readLeaf(row + "NotifType"),
			ReferenceList: parseReferenceList(e.readLeaf(row + "ReferenceList")),
			Recipient:     e.readLeaf(row + "Recipient"),
		}
		// Periodic legitimately allows an empty ReferenceList (no
		// value-change paths to scan; just an interval). Every other
		// NotifType is meaningless without a ReferenceList.
		if ref.NotifType != "" && ref.NotifType != NotifPeriodic && len(ref.ReferenceList) == 0 {
			e.Logger.Warn("usp Subscription row skipped: empty ReferenceList",
				"row", row, "notif_type", ref.NotifType, "id", ref.ID)
			continue
		}
		out = append(out, ref)
	}
	return out
}

func (e *Evaluator) readLeaf(path string) string {
	v, err := e.Tree.Get(path)
	if err != nil {
		return ""
	}
	return v.Raw
}

func parseReferenceList(raw string) []string {
	if raw == "" {
		return nil
	}
	out := []string{}
	for _, p := range strings.Fields(raw) {
		out = append(out, p)
	}
	return out
}

// NotifyValueChange dispatches a ValueChange Notify when at least one
// enabled Subscription's ReferenceList contains path.
func (e *Evaluator) NotifyValueChange(path, raw string) {
	e.mu.RLock()
	matches := append([]subscriptionRef(nil), e.valueChange[path]...)
	e.mu.RUnlock()
	for _, s := range matches {
		b := &notify.ValueChangeBuilder{SubscriptionID: s.ID}
		msg, err := b.Build(path, raw)
		if err != nil {
			e.Logger.Warn("usp value-change build failed", "path", path, "err", err.Error())
			continue
		}
		go e.send(msg, "ValueChange", "path", path)
	}
}

// NotifyObjectCreated dispatches an ObjectCreation Notify when at
// least one enabled Subscription's ReferenceList covers the path's
// prefix.
func (e *Evaluator) NotifyObjectCreated(objPath string, uniqueKeys map[string]string) {
	matches := e.matchPrefix(e.snapshotIndex(indexObjectCreation), objPath)
	if len(matches) == 0 {
		e.Logger.Debug("usp ObjectCreation suppressed (no matching subscription)", "obj_path", objPath)
		return
	}
	for _, s := range matches {
		b := &notify.ObjectCreationBuilder{SubscriptionID: s.ID}
		msg, err := b.Build(objPath, uniqueKeys)
		if err != nil {
			e.Logger.Warn("usp object-creation build failed", "err", err.Error())
			continue
		}
		go e.send(msg, "ObjectCreation", "obj_path", objPath)
	}
}

// NotifyObjectDeleted dispatches an ObjectDeletion Notify when at
// least one enabled Subscription's ReferenceList covers the path's
// prefix.
func (e *Evaluator) NotifyObjectDeleted(objPath string) {
	matches := e.matchPrefix(e.snapshotIndex(indexObjectDeletion), objPath)
	if len(matches) == 0 {
		e.Logger.Debug("usp ObjectDeletion suppressed (no matching subscription)", "obj_path", objPath)
		return
	}
	for _, s := range matches {
		b := &notify.ObjectDeletionBuilder{SubscriptionID: s.ID}
		msg, err := b.Build(objPath)
		if err != nil {
			e.Logger.Warn("usp object-deletion build failed", "err", err.Error())
			continue
		}
		go e.send(msg, "ObjectDeletion", "obj_path", objPath)
	}
}

// FireEvent dispatches an Event Notify (e.g. "Device.Boot!") to every
// subscription whose ReferenceList contains the named event.
func (e *Evaluator) FireEvent(eventName string) {
	e.mu.RLock()
	matches := append([]subscriptionRef(nil), e.eventIdx[eventName]...)
	e.mu.RUnlock()
	for _, s := range matches {
		b := &notify.BootEventBuilder{SubscriptionID: s.ID}
		msg, err := b.Build(e.Tree)
		if err != nil {
			e.Logger.Warn("usp event build failed", "event", eventName, "err", err.Error())
			continue
		}
		go e.send(msg, "Event", "event", eventName)
	}
}

type indexKind int

const (
	indexValueChange indexKind = iota
	indexObjectCreation
	indexObjectDeletion
	indexEvent
)

// snapshotIndex copies the named trigger index under the read lock so
// callers can iterate without holding the lock and without racing
// against rebuild()'s reassignment of the map field. Replaces the
// older snapshot(map) helper whose argument was read outside the lock.
func (e *Evaluator) snapshotIndex(kind indexKind) map[string][]subscriptionRef {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var m map[string][]subscriptionRef
	switch kind {
	case indexValueChange:
		m = e.valueChange
	case indexObjectCreation:
		m = e.objectCreation
	case indexObjectDeletion:
		m = e.objectDeletion
	case indexEvent:
		m = e.eventIdx
	}
	out := make(map[string][]subscriptionRef, len(m))
	for k, v := range m {
		out[k] = append([]subscriptionRef(nil), v...)
	}
	return out
}

func (e *Evaluator) matchPrefix(index map[string][]subscriptionRef, path string) []subscriptionRef {
	out := []subscriptionRef{}
	for prefix, refs := range index {
		if strings.HasPrefix(path, prefix) {
			out = append(out, refs...)
		}
	}
	return out
}

func (e *Evaluator) send(msg *uspproto.Msg, kind, k, v string) {
	wire, err := codec.WrapMessage(msg, e.AgentEID, e.ControllerEID)
	if err != nil {
		e.Logger.Warn("usp evaluator wrap failed", "kind", kind, k, v, "err", err.Error())
		return
	}
	if err := e.Adapter.Send(context.Background(), wire); err != nil {
		e.Logger.Warn("usp evaluator send failed", "kind", kind, k, v, "err", err.Error())
		return
	}
	e.Logger.Info("usp "+kind+" notify sent", k, v)
}

func (e *Evaluator) reschedulePeriodic(subs []subscriptionRef) {
	if e.Scheduler == nil {
		return
	}

	wanted := map[string]subscriptionRef{}
	for _, s := range subs {
		if s.NotifType == NotifPeriodic && s.ID != "" {
			wanted[s.ID] = s
		}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Cancel periodic schedules that are no longer wanted.
	for id, cancel := range e.periodicCancels {
		if _, keep := wanted[id]; !keep {
			if cancel != nil {
				cancel()
			}
			delete(e.periodicCancels, id)
		}
	}

	// Arm new periodic schedules.
	for id, s := range wanted {
		if _, exists := e.periodicCancels[id]; exists {
			continue
		}
		period := e.readPeriodSeconds(id)
		if period <= 0 {
			continue
		}
		e.periodicCancels[id] = e.armPeriodic(id, s, period)
	}
}

func (e *Evaluator) readPeriodSeconds(subID string) int {
	children, err := e.Tree.Children(SubscriptionTablePath)
	if err != nil {
		return 0
	}
	for _, c := range children {
		row := c.Name
		if e.readLeaf(row+"ID") != subID {
			continue
		}
		raw := e.readLeaf(row + "Period")
		if raw == "" {
			return 0
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0
		}
		return n
	}
	return 0
}

func (e *Evaluator) armPeriodic(subID string, s subscriptionRef, periodSeconds int) func() {
	period := time.Duration(periodSeconds) * time.Second
	cpeKey := e.CPEID + ":usp-periodic:" + subID
	var cancel func()
	var armNext func()
	armNext = func() {
		jitter := time.Duration(float64(period) * (0.9 + 0.2*e.RNG.Float64()))
		cancel = e.Scheduler.ScheduleOnce(cpeKey, jitter, func(_ context.Context) error {
			for _, p := range s.ReferenceList {
				raw := e.readLeaf(p)
				b := &notify.ValueChangeBuilder{SubscriptionID: subID}
				msg, err := b.Build(p, raw)
				if err != nil {
					e.Logger.Warn("usp periodic build failed", "sub_id", subID, "path", p, "err", err.Error())
					continue
				}
				go e.send(msg, "Periodic", "path", p)
			}
			armNext()
			return nil
		})
	}
	armNext()
	return func() {
		if cancel != nil {
			cancel()
		}
	}
}
