package handlers_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
	"github.com/herder-labs/cpe-labs/internal/usp/handlers"
	"github.com/herder-labs/cpe-labs/internal/usp/session"
)

func TestDeleteHandlerHappyPath(t *testing.T) {
	tree, _ := loadWifiSSID(t)

	var notifies atomic.Int32
	var lastPath atomic.Value
	h := handlers.NewDelete(tree, func(p string) {
		notifies.Add(1)
		lastPath.Store(p)
	})

	req := buildDeleteRequest("Device.WiFi.SSID.1.")
	resp, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	results := resp.GetBody().GetResponse().GetDeleteResp().GetDeletedObjResults()
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	success := results[0].GetOperStatus().GetOperSuccess()
	if success == nil {
		t.Fatalf("OperSuccess nil")
	}
	if len(success.GetAffectedPaths()) != 1 || success.GetAffectedPaths()[0] != "Device.WiFi.SSID.1." {
		t.Errorf("AffectedPaths=%v", success.GetAffectedPaths())
	}
	if _, err := tree.Get("Device.WiFi.SSID.1.SSID"); err == nil {
		t.Errorf("SSID.1 still resolvable after Delete")
	}

	for i := 0; i < 50; i++ {
		if notifies.Load() == 1 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if notifies.Load() != 1 {
		t.Errorf("ObjectDeletion notifier calls=%d want 1", notifies.Load())
	}
	if lastPath.Load().(string) != "Device.WiFi.SSID.1." {
		t.Errorf("lastPath=%q", lastPath.Load())
	}
}

func TestDeleteHandlerMissingInstance(t *testing.T) {
	tree, _ := loadWifiSSID(t)
	h := handlers.NewDelete(tree, func(p string) { t.Fatalf("notifier fired for missing instance") })
	req := buildDeleteRequest("Device.WiFi.SSID.99.")
	resp, _ := h.Handle(context.Background(), req)
	fail := resp.GetBody().GetResponse().GetDeleteResp().GetDeletedObjResults()[0].GetOperStatus().GetOperFailure()
	if fail == nil {
		t.Fatalf("expected OperFailure")
	}
	if fail.GetErrCode() != session.USPErrObjectDoesNotExist {
		t.Errorf("ErrCode=%d want %d", fail.GetErrCode(), session.USPErrObjectDoesNotExist)
	}
	time.Sleep(20 * time.Millisecond)
}

func TestDeleteHandlerRejectsPathWithoutTrailingDot(t *testing.T) {
	tree, _ := loadWifiSSID(t)
	h := handlers.NewDelete(tree, nil)
	req := buildDeleteRequest("Device.WiFi.SSID.1")
	resp, _ := h.Handle(context.Background(), req)
	fail := resp.GetBody().GetResponse().GetDeleteResp().GetDeletedObjResults()[0].GetOperStatus().GetOperFailure()
	if fail.GetErrCode() != session.USPErrInvalidPath {
		t.Errorf("ErrCode=%d want %d", fail.GetErrCode(), session.USPErrInvalidPath)
	}
}

func TestDeleteHandlerMultiplePaths(t *testing.T) {
	body := strings.Replace(wifiSSIDProfile, "instances: 1", "instances: 3", 1)
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	h := handlers.NewDelete(p.Tree, nil)
	req := buildDeleteRequest("Device.WiFi.SSID.1.", "Device.WiFi.SSID.3.")
	resp, _ := h.Handle(context.Background(), req)
	results := resp.GetBody().GetResponse().GetDeleteResp().GetDeletedObjResults()
	if len(results) != 2 {
		t.Fatalf("len=%d want 2", len(results))
	}
	for i, r := range results {
		if r.GetOperStatus().GetOperSuccess() == nil {
			t.Errorf("[%d] not success", i)
		}
	}
	// SSID.2. survives.
	if _, err := p.Tree.Get("Device.WiFi.SSID.2.SSID"); err != nil {
		t.Errorf("SSID.2 unexpectedly missing: %v", err)
	}
}

func buildDeleteRequest(paths ...string) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{MsgId: "test", MsgType: uspproto.Header_DELETE},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Delete{
						Delete: &uspproto.Delete{ObjPaths: paths},
					},
				},
			},
		},
	}
}
