package paramtree_test

import (
	"strings"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/paramtree"
)

const subTableUSPProfile = `
parameters:
  - path: Device.DeviceInfo.Manufacturer
    value: "ACME"
  - path: Device.DeviceInfo.ManufacturerOUI
    value: "AABBCC"
  - path: Device.DeviceInfo.ProductClass
    value: "Generic"
  - path: Device.DeviceInfo.SerialNumber
    value: "SN1"

usp:
  enable: true
  broker:
    address: nats
`

func TestLoadProfileUSPInstallsSubscriptionTable(t *testing.T) {
	t.Parallel()
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(subTableUSPProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}

	if !p.Tree.IsAddDeletable("Device.LocalAgent.Subscription") {
		t.Errorf("Device.LocalAgent.Subscription should be add-deletable")
	}

	checks := map[string]string{
		"Device.LocalAgent.Subscription.1.Enable":        "true",
		"Device.LocalAgent.Subscription.1.ID":            "default-boot-event-ACS",
		"Device.LocalAgent.Subscription.1.Recipient":     "Device.LocalAgent.Controller.1",
		"Device.LocalAgent.Subscription.1.NotifType":     "Event",
		"Device.LocalAgent.Subscription.1.ReferenceList": "Device.Boot!",
		"Device.LocalAgent.Subscription.1.Persistent":    "true",
	}
	for path, want := range checks {
		v, err := p.Tree.Get(path)
		if err != nil {
			t.Errorf("Get(%s): %v", path, err)
			continue
		}
		if v.Raw != want {
			t.Errorf("%s = %q want %q", path, v.Raw, want)
		}
	}

	keys, ok := p.UniqueKeys["Device.LocalAgent.Subscription."]
	if !ok {
		t.Errorf("UniqueKeys missing Device.LocalAgent.Subscription.")
	} else if len(keys) != 1 || keys[0][0] != "ID" {
		t.Errorf("UniqueKeys=%+v want [[ID]]", keys)
	}
}

func TestLoadProfileUSPSubscriptionRowIsWritable(t *testing.T) {
	t.Parallel()
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(subTableUSPProfile), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if err := p.Tree.Set("Device.LocalAgent.Subscription.1.Enable",
		paramtree.Value{Type: paramtree.TypeBoolean, Raw: "false", Writable: true}); err != nil {
		t.Fatalf("Set Enable: %v", err)
	}
	v, _ := p.Tree.Get("Device.LocalAgent.Subscription.1.Enable")
	if v.Raw != "false" {
		t.Errorf("after Set, Enable=%q want false", v.Raw)
	}
}

func TestLoadProfileNoUSPDoesNotInstallSubscriptionTable(t *testing.T) {
	t.Parallel()
	body := `parameters:
  - path: Device.X
    value: "y"`
	p, err := paramtree.LoadProfileFromReader(strings.NewReader(body), "<test>")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if _, err := p.Tree.Get("Device.LocalAgent.Subscription.1.Enable"); err == nil {
		t.Errorf("Subscription table should not exist when USP is disabled")
	}
}
