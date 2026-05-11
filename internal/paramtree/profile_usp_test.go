package paramtree_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	"github.com/herder-labs/cpe-labs/internal/paramtree"
)

const uspMinDeviceInfo = `parameters:
  - path: Device.DeviceInfo.Manufacturer
    value: "ACME"
  - path: Device.DeviceInfo.ManufacturerOUI
    value: "AABBCC"
  - path: Device.DeviceInfo.ProductClass
    value: "Generic"
  - path: Device.DeviceInfo.SerialNumber
    value: "SN1"
`

func TestLoadProfileUSPHappyPath(t *testing.T) {
	t.Parallel()
	body := uspMinDeviceInfo + `
usp:
  enable: true
  endpointID:
    scheme: os
    ouiPath: Device.DeviceInfo.ManufacturerOUI
    serialPath: Device.DeviceInfo.SerialNumber
  controllerEndpointID: "self::openacs"
  broker:
    address: nats
    port: 1883
    protocolVersion: "3.1.1"
  dataModels: [device]
`
	p, err := loadProfileFromStringFull(t, body)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if !p.USP.Enable {
		t.Fatalf("Enable=false")
	}
	if p.USP.Broker.Address != "nats" || p.USP.Broker.Port != 1883 || p.USP.Broker.ProtocolVersion != "3.1.1" {
		t.Fatalf("Broker=%+v", p.USP.Broker)
	}
	if p.USP.ControllerEndpointID != "self::openacs" {
		t.Fatalf("ControllerEndpointID=%q", p.USP.ControllerEndpointID)
	}
	if p.USP.EndpointID.Scheme != "os" {
		t.Fatalf("Scheme=%q", p.USP.EndpointID.Scheme)
	}
	if !p.USP.Broker.CleanSession {
		t.Fatalf("CleanSession default should be true")
	}
}

func TestLoadProfileUSPAppliesDefaults(t *testing.T) {
	t.Parallel()
	body := uspMinDeviceInfo + `
usp:
  enable: true
  broker:
    address: nats
`
	p, err := loadProfileFromStringFull(t, body)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if p.USP.Broker.Port != 1883 {
		t.Fatalf("Port default = %d want 1883", p.USP.Broker.Port)
	}
	if p.USP.Broker.ProtocolVersion != "3.1.1" {
		t.Fatalf("ProtocolVersion default = %q want 3.1.1", p.USP.Broker.ProtocolVersion)
	}
	if p.USP.Broker.KeepAliveSeconds != 60 {
		t.Fatalf("KeepAliveSeconds default = %d want 60", p.USP.Broker.KeepAliveSeconds)
	}
	if p.USP.ControllerEndpointID != "self::openacs" {
		t.Fatalf("ControllerEndpointID default = %q", p.USP.ControllerEndpointID)
	}
	if p.USP.EndpointID.OUIPath != "Device.DeviceInfo.ManufacturerOUI" {
		t.Fatalf("OUIPath default = %q", p.USP.EndpointID.OUIPath)
	}
	if p.USP.EndpointID.SerialPath != "Device.DeviceInfo.SerialNumber" {
		t.Fatalf("SerialPath default = %q", p.USP.EndpointID.SerialPath)
	}
	if got := strings.Join(p.USP.DataModels, ","); got != "device" {
		t.Fatalf("DataModels default = %q", got)
	}
}

func TestLoadProfileUSPRejectsMQTT5(t *testing.T) {
	t.Parallel()
	body := uspMinDeviceInfo + `
usp:
  enable: true
  broker:
    address: nats
    protocolVersion: "5.0"
`
	assertInvalidArg(t, loadProfileErr(t, body))
}

func TestLoadProfileUSPRequiresBrokerAddress(t *testing.T) {
	t.Parallel()
	body := uspMinDeviceInfo + `
usp:
  enable: true
  broker:
    port: 1883
`
	assertInvalidArg(t, loadProfileErr(t, body))
}

func TestLoadProfileUSPRejectsMalformedControllerEID(t *testing.T) {
	t.Parallel()
	body := uspMinDeviceInfo + `
usp:
  enable: true
  controllerEndpointID: "no-separator-here"
  broker:
    address: nats
`
	assertInvalidArg(t, loadProfileErr(t, body))
}

func TestLoadProfileUSPRejectsMissingOUIPath(t *testing.T) {
	t.Parallel()
	body := uspMinDeviceInfo + `
usp:
  enable: true
  endpointID:
    ouiPath: Device.Bogus.OUI
  broker:
    address: nats
`
	assertInvalidArg(t, loadProfileErr(t, body))
}

func TestLoadProfileUSPInstallsInternalRebootCauseLeaf(t *testing.T) {
	t.Parallel()
	body := uspMinDeviceInfo + `
usp:
  enable: true
  broker:
    address: nats
`
	p, err := loadProfileFromStringFull(t, body)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	v, err := p.Tree.Get("Internal.Reboot.Cause")
	if err != nil {
		t.Fatalf("Get Internal.Reboot.Cause: %v", err)
	}
	if v.Raw != "LocalFactoryReset" {
		t.Fatalf("Internal.Reboot.Cause = %q want LocalFactoryReset", v.Raw)
	}
	if v.Type != paramtree.TypeString {
		t.Fatalf("Type = %v", v.Type)
	}
	if v.Writable {
		t.Fatalf("Internal.Reboot.Cause must not be operator-writable")
	}
}

func TestLoadProfileUSPDisabledDoesNotInstallRebootCauseLeaf(t *testing.T) {
	t.Parallel()
	body := uspMinDeviceInfo + `
usp:
  enable: false
`
	p, err := loadProfileFromStringFull(t, body)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if _, err := p.Tree.Get("Internal.Reboot.Cause"); err == nil {
		t.Fatalf("Internal.Reboot.Cause should not exist when USP is disabled")
	}
}

func TestLoadProfileUSPOmittedKeepsZeroValue(t *testing.T) {
	t.Parallel()
	p, err := loadProfileFromStringFull(t, uspMinDeviceInfo)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if !p.USP.IsZero() {
		t.Fatalf("expected zero USPConfig, got %+v", p.USP)
	}
	if p.USP.RequiresDaemon() {
		t.Fatalf("zero USPConfig should not require daemon mode")
	}
}

func loadProfileErr(t *testing.T, body string) error {
	t.Helper()
	_, err := loadProfileFromStringFull(t, body)
	return err
}

func assertInvalidArg(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var ce *cpeerr.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *cpeerr.Error, got %T: %v", err, err)
	}
	if ce.Kind != cpeerr.KindInvalidArgument {
		t.Fatalf("Kind=%v want KindInvalidArgument: %v", ce.Kind, err)
	}
}
