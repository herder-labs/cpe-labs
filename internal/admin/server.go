// Package admin serves the operator-facing introspection endpoints
// that ride alongside the Prometheus /metrics scrape: per-CPE state,
// force-CR, and fault-injection hooks. v0 is unauthenticated and
// bound to operator-local addresses; treat any external exposure as
// a trust-boundary failure.
package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CPEStackInspector exposes the per-CPE state the admin server needs.
// cmd/cpe-sim's cpeStack satisfies it structurally; keeping the
// contract in this package prevents an internal/admin → cmd/cpe-sim
// cycle.
type CPEStackInspector interface {
	ID() string
	Serial() string
	LifecycleState() string
	LastEvent() (name string, at time.Time)
	RecentEvents() []EventTail
	TreeSample() map[string]string
	USPSubscriptionSummary() []USPSubscription
}

// EventTail is one row of the recent-events ring buffer.
type EventTail struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}

// USPSubscription is one row of the per-CPE Subscription table summary.
type USPSubscription struct {
	ID            string `json:"id"`
	NotifType     string `json:"notif_type"`
	ReferenceList string `json:"reference_list"`
	Recipient     string `json:"recipient"`
}

// CRForcer triggers a synthetic Connection-Request session for a CPE.
type CRForcer interface {
	ForceCR(cpeID string) error
}

// FaultInjector queues a fault to be returned on the next CWMP session.
type FaultInjector interface {
	InjectFault(cpeID string, code int) error
}

// Server bundles the admin routes and the inspector/CR/fault hooks.
type Server struct {
	mu        sync.RWMutex
	stacks    []CPEStackInspector
	indexByID map[string]CPEStackInspector
	cr        CRForcer
	faults    FaultInjector
}

// NewServer returns a Server with the given stacks indexed by CPE ID.
// stacks may be nil; SetStacks updates the slice at runtime.
func NewServer(stacks []CPEStackInspector, cr CRForcer, faults FaultInjector) *Server {
	s := &Server{cr: cr, faults: faults}
	s.SetStacks(stacks)
	return s
}

// SetStacks replaces the inspector slice atomically.
func (s *Server) SetStacks(stacks []CPEStackInspector) {
	idx := make(map[string]CPEStackInspector, len(stacks))
	for _, st := range stacks {
		idx[st.ID()] = st
	}
	s.mu.Lock()
	s.stacks = stacks
	s.indexByID = idx
	s.mu.Unlock()
}

// Routes returns an http.Handler mounting every admin route. Callers
// usually mount it under /admin via http.ServeMux.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/cpes", s.handleList)
	mux.HandleFunc("/admin/cpes/", s.handleCPEPath)
	return mux
}

type cpeRow struct {
	ID             string    `json:"id"`
	Serial         string    `json:"serial"`
	LifecycleState string    `json:"lifecycle_state"`
	LastEvent      string    `json:"last_event"`
	LastEventAt    time.Time `json:"last_event_at"`
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	rows := make([]cpeRow, 0, len(s.stacks))
	for _, st := range s.stacks {
		name, at := st.LastEvent()
		rows = append(rows, cpeRow{
			ID:             st.ID(),
			Serial:         st.Serial(),
			LifecycleState: st.LifecycleState(),
			LastEvent:      name,
			LastEventAt:    at,
		})
	}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, rows)
}

type cpeDetail struct {
	cpeRow
	RecentEvents     []EventTail       `json:"recent_events"`
	TreeSample       map[string]string `json:"tree_sample"`
	USPSubscriptions []USPSubscription `json:"usp_subscriptions"`
}

// handleCPEPath dispatches /admin/cpes/{id}, /admin/cpes/{id}/cr, and
// /admin/cpes/{id}/fault/{code}.
func (s *Server) handleCPEPath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/admin/cpes/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	segs := strings.Split(rest, "/")
	id := segs[0]

	s.mu.RLock()
	stack, ok := s.indexByID[id]
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case len(segs) == 1:
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name, at := stack.LastEvent()
		writeJSON(w, http.StatusOK, cpeDetail{
			cpeRow: cpeRow{
				ID:             stack.ID(),
				Serial:         stack.Serial(),
				LifecycleState: stack.LifecycleState(),
				LastEvent:      name,
				LastEventAt:    at,
			},
			RecentEvents:     stack.RecentEvents(),
			TreeSample:       stack.TreeSample(),
			USPSubscriptions: stack.USPSubscriptionSummary(),
		})
	case len(segs) == 2 && segs[1] == "cr":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.cr == nil {
			http.Error(w, "force-CR not configured", http.StatusServiceUnavailable)
			return
		}
		if err := s.cr.ForceCR(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"cr_url":     fmt.Sprintf("/admin/cpes/%s/cr", id),
			"invoked_at": time.Now().UTC(),
		})
	case len(segs) == 3 && segs[1] == "fault":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.faults == nil {
			http.Error(w, "fault-injection not configured", http.StatusServiceUnavailable)
			return
		}
		code, err := parseFaultCode(segs[2])
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.faults.InjectFault(id, code); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"cpe_id":     id,
			"fault_code": code,
			"invoked_at": time.Now().UTC(),
		})
	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func parseFaultCode(s string) (int, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("fault code is empty")
	}
	var v int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("fault code must be numeric, got %q", s)
		}
		v = v*10 + int(c-'0')
	}
	return v, nil
}
