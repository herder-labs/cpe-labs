package notify_test

import (
	"errors"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
	"github.com/herder-labs/cpe-labs/internal/usp/notify"
)

func TestOnBoardRequestBuildHappyPath(t *testing.T) {
	tree := buildTreeWithDeviceInfo(t, "001122", "HomeGateway", "ABC123")
	b := &notify.OnBoardRequestBuilder{}

	msg, err := b.Build(tree)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if msg.GetHeader().GetMsgId() == "" {
		t.Fatalf("MsgId empty")
	}
	notif := msg.GetBody().GetRequest().GetNotify()
	if notif == nil {
		t.Fatalf("Notify nil")
	}
	if notif.GetSubscriptionId() != "" {
		t.Fatalf("OnBoardRequest subscription_id must be empty, got %q", notif.GetSubscriptionId())
	}
	if notif.GetSendResp() {
		t.Fatalf("OnBoardRequest send_resp must be false")
	}
	req := notif.GetOnBoardReq()
	if req == nil {
		t.Fatalf("OnBoardReq nil")
	}
	if req.GetOui() != "001122" {
		t.Fatalf("Oui=%q", req.GetOui())
	}
	if req.GetProductClass() != "HomeGateway" {
		t.Fatalf("ProductClass=%q", req.GetProductClass())
	}
	if req.GetSerialNumber() != "ABC123" {
		t.Fatalf("SerialNumber=%q", req.GetSerialNumber())
	}
	if req.GetAgentSupportedProtocolVersions() != "1.5" {
		t.Fatalf("AgentSupportedProtocolVersions=%q", req.GetAgentSupportedProtocolVersions())
	}
}

func TestOnBoardRequestBuildCustomVersion(t *testing.T) {
	tree := buildTreeWithDeviceInfo(t, "AABBCC", "Generic", "SN1")
	b := &notify.OnBoardRequestBuilder{AgentVersion: "1.4"}

	msg, err := b.Build(tree)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := msg.GetBody().GetRequest().GetNotify().GetOnBoardReq().GetAgentSupportedProtocolVersions(); got != "1.4" {
		t.Fatalf("AgentSupportedProtocolVersions=%q want 1.4", got)
	}
}

func TestOnBoardRequestRejectsMissingSerial(t *testing.T) {
	tree := paramtree.New()
	mustMount(t, tree, "Device.DeviceInfo.ManufacturerOUI", "001122")
	mustMount(t, tree, "Device.DeviceInfo.ProductClass", "Generic")

	_, err := (&notify.OnBoardRequestBuilder{}).Build(tree)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var ce *cpeerr.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *cpeerr.Error, got %T", err)
	}
	if ce.Kind != cpeerr.KindNotFound {
		t.Fatalf("Kind=%v want KindNotFound", ce.Kind)
	}
}

func TestBootEventBuildHappyPath(t *testing.T) {
	tree := paramtree.New()
	b := &notify.BootEventBuilder{}

	msg, err := b.Build(tree)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	notif := msg.GetBody().GetRequest().GetNotify()
	if notif.GetSubscriptionId() != notify.DefaultBootSubscriptionID {
		t.Fatalf("SubscriptionId=%q want %q", notif.GetSubscriptionId(), notify.DefaultBootSubscriptionID)
	}
	ev := notif.GetEvent()
	if ev == nil {
		t.Fatalf("Event nil")
	}
	if ev.GetObjPath() != "Device." {
		t.Fatalf("ObjPath=%q", ev.GetObjPath())
	}
	if ev.GetEventName() != "Boot!" {
		t.Fatalf("EventName=%q", ev.GetEventName())
	}
}

func TestBootEventBuildCustomSubscription(t *testing.T) {
	msg, err := (&notify.BootEventBuilder{SubscriptionID: "operator-boot-sub"}).Build(paramtree.New())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := msg.GetBody().GetRequest().GetNotify().GetSubscriptionId(); got != "operator-boot-sub" {
		t.Fatalf("SubscriptionId=%q", got)
	}
}

func buildTreeWithDeviceInfo(t *testing.T, oui, productClass, serial string) *paramtree.Tree {
	t.Helper()
	tree := paramtree.New()
	mustMount(t, tree, "Device.DeviceInfo.ManufacturerOUI", oui)
	mustMount(t, tree, "Device.DeviceInfo.ProductClass", productClass)
	mustMount(t, tree, "Device.DeviceInfo.SerialNumber", serial)
	return tree
}

func mustMount(t *testing.T, tree *paramtree.Tree, path, raw string) {
	t.Helper()
	if err := tree.Mount(path, paramtree.NewLeaf(paramtree.Value{Type: paramtree.TypeString, Raw: raw, Writable: false})); err != nil {
		t.Fatalf("Mount %s: %v", path, err)
	}
}
