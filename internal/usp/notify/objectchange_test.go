package notify_test

import (
	"testing"

	"github.com/herder-labs/cpe-labs/internal/usp/notify"
)

func TestObjectCreationBuilderHappyPath(t *testing.T) {
	b := &notify.ObjectCreationBuilder{}
	msg, err := b.Build("Device.WiFi.SSID.42.", map[string]string{"SSID": "HomeNet", "BSSID": "AABBCC112233"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if msg.GetHeader().GetMsgId() == "" {
		t.Errorf("MsgId empty")
	}
	notif := msg.GetBody().GetRequest().GetNotify()
	if notif.GetSubscriptionId() != "" {
		t.Errorf("SubscriptionId=%q, want empty in v0", notif.GetSubscriptionId())
	}
	oc := notif.GetObjCreation()
	if oc == nil {
		t.Fatalf("ObjCreation nil")
	}
	if oc.GetObjPath() != "Device.WiFi.SSID.42." {
		t.Errorf("ObjPath=%q", oc.GetObjPath())
	}
	if oc.GetUniqueKeys()["SSID"] != "HomeNet" {
		t.Errorf("UniqueKeys[SSID]=%q", oc.GetUniqueKeys()["SSID"])
	}
	if oc.GetUniqueKeys()["BSSID"] != "AABBCC112233" {
		t.Errorf("UniqueKeys[BSSID]=%q", oc.GetUniqueKeys()["BSSID"])
	}
}

func TestObjectCreationBuilderEmptyUniqueKeys(t *testing.T) {
	b := &notify.ObjectCreationBuilder{}
	msg, err := b.Build("Device.WiFi.SSID.1.", nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	oc := msg.GetBody().GetRequest().GetNotify().GetObjCreation()
	if len(oc.GetUniqueKeys()) != 0 {
		t.Errorf("UniqueKeys=%+v want empty", oc.GetUniqueKeys())
	}
}

func TestObjectCreationBuilderCustomSubscription(t *testing.T) {
	b := &notify.ObjectCreationBuilder{SubscriptionID: "ssid-creates"}
	msg, _ := b.Build("Device.WiFi.SSID.1.", nil)
	if got := msg.GetBody().GetRequest().GetNotify().GetSubscriptionId(); got != "ssid-creates" {
		t.Errorf("SubscriptionId=%q", got)
	}
}

func TestObjectDeletionBuilderHappyPath(t *testing.T) {
	b := &notify.ObjectDeletionBuilder{}
	msg, err := b.Build("Device.WiFi.SSID.42.")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	od := msg.GetBody().GetRequest().GetNotify().GetObjDeletion()
	if od == nil {
		t.Fatalf("ObjDeletion nil")
	}
	if od.GetObjPath() != "Device.WiFi.SSID.42." {
		t.Errorf("ObjPath=%q", od.GetObjPath())
	}
}
