package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeStack struct {
	id, serial, state string
	lastName          string
	lastAt            time.Time
	events            []EventTail
	tree              map[string]string
	subs              []USPSubscription
}

func (f *fakeStack) ID() string                          { return f.id }
func (f *fakeStack) Serial() string                      { return f.serial }
func (f *fakeStack) LifecycleState() string              { return f.state }
func (f *fakeStack) LastEvent() (string, time.Time)      { return f.lastName, f.lastAt }
func (f *fakeStack) RecentEvents() []EventTail           { return f.events }
func (f *fakeStack) TreeSample() map[string]string       { return f.tree }
func (f *fakeStack) USPSubscriptionSummary() []USPSubscription { return f.subs }

type fakeCR struct {
	mu      sync.Mutex
	calls   []string
	failOn  string
	failErr string
}

func (f *fakeCR) ForceCR(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id)
	if id == f.failOn {
		return &stringErr{s: f.failErr}
	}
	return nil
}

type fakeFaults struct {
	mu    sync.Mutex
	calls []struct {
		id   string
		code int
	}
}

func (f *fakeFaults) InjectFault(id string, code int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		id   string
		code int
	}{id, code})
	return nil
}

type stringErr struct{ s string }

func (e *stringErr) Error() string { return e.s }

func newTestServer(t *testing.T, stacks []CPEStackInspector, cr CRForcer, fi FaultInjector) *httptest.Server {
	t.Helper()
	s := NewServer(stacks, cr, fi)
	return httptest.NewServer(s.Routes())
}

func TestListCPEs(t *testing.T) {
	stacks := []CPEStackInspector{
		&fakeStack{id: "cpe-1", serial: "AA001", state: "ready", lastName: "2 PERIODIC", lastAt: time.Unix(1700000000, 0).UTC()},
		&fakeStack{id: "cpe-2", serial: "AA002", state: "bootstrapping"},
	}
	srv := newTestServer(t, stacks, nil, nil)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/admin/cpes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var rows []cpeRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].ID != "cpe-1" || rows[0].LifecycleState != "ready" {
		t.Errorf("row 0 unexpected: %+v", rows[0])
	}
}

func TestGetCPEDetail(t *testing.T) {
	stack := &fakeStack{
		id: "cpe-1", serial: "AA001", state: "ready",
		lastName: "2 PERIODIC", lastAt: time.Unix(1700000000, 0).UTC(),
		events: []EventTail{{Name: "0 BOOTSTRAP", At: time.Unix(1699999000, 0).UTC()}},
		tree:   map[string]string{"Device.DeviceInfo.Manufacturer": "ACME"},
		subs:   []USPSubscription{{ID: "1", NotifType: "ValueChange", ReferenceList: "Device.WiFi.SSID", Recipient: "self::openacs"}},
	}
	srv := newTestServer(t, []CPEStackInspector{stack}, nil, nil)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/admin/cpes/cpe-1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var detail cpeDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID != "cpe-1" {
		t.Errorf("ID = %q", detail.ID)
	}
	if len(detail.RecentEvents) != 1 || detail.RecentEvents[0].Name != "0 BOOTSTRAP" {
		t.Errorf("RecentEvents = %+v", detail.RecentEvents)
	}
	if detail.TreeSample["Device.DeviceInfo.Manufacturer"] != "ACME" {
		t.Errorf("TreeSample = %+v", detail.TreeSample)
	}
	if len(detail.USPSubscriptions) != 1 {
		t.Errorf("USPSubscriptions = %+v", detail.USPSubscriptions)
	}
}

func TestGetCPE_NotFound(t *testing.T) {
	srv := newTestServer(t, nil, nil, nil)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/admin/cpes/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
}

func TestForceCR(t *testing.T) {
	cr := &fakeCR{}
	srv := newTestServer(t, []CPEStackInspector{&fakeStack{id: "cpe-1"}}, cr, nil)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/admin/cpes/cpe-1/cr", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("status %d, want 202", resp.StatusCode)
	}
	if len(cr.calls) != 1 || cr.calls[0] != "cpe-1" {
		t.Errorf("ForceCR calls = %v", cr.calls)
	}
}

func TestForceCR_NotConfigured(t *testing.T) {
	srv := newTestServer(t, []CPEStackInspector{&fakeStack{id: "cpe-1"}}, nil, nil)
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/admin/cpes/cpe-1/cr", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("status %d, want 503", resp.StatusCode)
	}
}

func TestInjectFault(t *testing.T) {
	fi := &fakeFaults{}
	srv := newTestServer(t, []CPEStackInspector{&fakeStack{id: "cpe-1"}}, nil, fi)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/admin/cpes/cpe-1/fault/9002", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("status %d, want 202", resp.StatusCode)
	}
	if len(fi.calls) != 1 || fi.calls[0].code != 9002 {
		t.Errorf("InjectFault calls = %v", fi.calls)
	}

	bad, _ := http.Post(srv.URL+"/admin/cpes/cpe-1/fault/notanumber", "", nil)
	defer bad.Body.Close()
	if bad.StatusCode != 400 {
		t.Errorf("bad-code status %d, want 400", bad.StatusCode)
	}
}

func TestSetStacks_RuntimeUpdate(t *testing.T) {
	s := NewServer(nil, nil, nil)
	mux := s.Routes()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/cpes/cpe-1", nil))
	if rec.Code != 404 {
		t.Fatalf("initial GET should 404, got %d", rec.Code)
	}

	s.SetStacks([]CPEStackInspector{&fakeStack{id: "cpe-1", state: "ready"}})

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/cpes/cpe-1", nil))
	if rec.Code != 200 {
		t.Fatalf("post-update GET status %d, body %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ready"`) {
		t.Errorf("body missing state: %s", rec.Body.String())
	}
}
