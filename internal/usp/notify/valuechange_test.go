package notify_test

import (
	"testing"

	"github.com/herder-labs/cpe-labs/internal/usp/notify"
)

func TestValueChangeBuilder(t *testing.T) {
	b := &notify.ValueChangeBuilder{SubscriptionID: "vc-sub-1"}
	msg, err := b.Build("Device.WiFi.Radio.1.Channel", "11")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	n := msg.GetBody().GetRequest().GetNotify()
	if n.GetSubscriptionId() != "vc-sub-1" {
		t.Errorf("SubscriptionId=%q", n.GetSubscriptionId())
	}
	vc := n.GetValueChange()
	if vc == nil {
		t.Fatalf("ValueChange nil")
	}
	if vc.GetParamPath() != "Device.WiFi.Radio.1.Channel" {
		t.Errorf("ParamPath=%q", vc.GetParamPath())
	}
	if vc.GetParamValue() != "11" {
		t.Errorf("ParamValue=%q", vc.GetParamValue())
	}
}
