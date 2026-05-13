//go:build acceptance

package harness

import (
	"bytes"
	"strings"
	"testing"
)

func TestNormalizeCWMP_ReplacesID(t *testing.T) {
	in := []byte(`<soapenv:Header><cwmp:ID soapenv:mustUnderstand="1">42</cwmp:ID></soapenv:Header>`)
	got := NormalizeCWMP(in)
	if !bytes.Contains(got, []byte(`{ID}`)) {
		t.Errorf("expected {ID} substitution, got %s", got)
	}
	if bytes.Contains(got, []byte(`>42<`)) {
		t.Errorf("expected 42 to be replaced, got %s", got)
	}
}

func TestNormalizeCWMP_ReplacesCurrentTime(t *testing.T) {
	in := []byte(`<CurrentTime>2026-05-13T08:00:00Z</CurrentTime>`)
	got := NormalizeCWMP(in)
	if !bytes.Equal(got, []byte(`<CurrentTime>{TIMESTAMP}</CurrentTime>`)) {
		t.Errorf("CurrentTime not normalized: %s", got)
	}
}

func TestNormalizeCWMP_ReplacesConnectionRequestURL(t *testing.T) {
	in := []byte(`<ConnectionRequestURL>http://127.0.0.1:54321/cr</ConnectionRequestURL>`)
	got := NormalizeCWMP(in)
	if !bytes.Equal(got, []byte(`<ConnectionRequestURL>{CR_URL}</ConnectionRequestURL>`)) {
		t.Errorf("CR URL not normalized: %s", got)
	}
}

func TestNormalizeCWMP_PreservesDeviceId(t *testing.T) {
	in := []byte(`<DeviceId>
		<Manufacturer>ACME</Manufacturer>
		<OUI>001122</OUI>
		<ProductClass>Test</ProductClass>
		<SerialNumber>SN1</SerialNumber>
	</DeviceId>`)
	got := NormalizeCWMP(in)
	if !bytes.Equal(got, in) {
		t.Errorf("DeviceId modified by normalizer:\n--- want\n%s\n--- got\n%s", in, got)
	}
}

func TestNormalizeCWMP_PreservesRetryCount(t *testing.T) {
	in := []byte(`<RetryCount>3</RetryCount>`)
	got := NormalizeCWMP(in)
	if !bytes.Equal(got, in) {
		t.Errorf("RetryCount must be preserved (deterministic field); got %s", got)
	}
}

func TestNormalizeCWMP_PreservesEventCode(t *testing.T) {
	in := []byte(`<EventCode>0 BOOTSTRAP</EventCode>`)
	got := NormalizeCWMP(in)
	if !bytes.Equal(got, in) {
		t.Errorf("EventCode modified by normalizer; got %s", got)
	}
}

func TestNormalizeCWMP_FullEnvelopeRoundTrip(t *testing.T) {
	in := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="..." xmlns:cwmp="urn:dslforum-org:cwmp-1-1">
  <soapenv:Header><cwmp:ID soapenv:mustUnderstand="1">17</cwmp:ID></soapenv:Header>
  <soapenv:Body>
    <cwmp:Inform>
      <DeviceId>
        <Manufacturer>ACME</Manufacturer>
        <OUI>AABBCC</OUI>
        <ProductClass>GW</ProductClass>
        <SerialNumber>SN-1</SerialNumber>
      </DeviceId>
      <Event>
        <EventStruct><EventCode>0 BOOTSTRAP</EventCode><CommandKey></CommandKey></EventStruct>
      </Event>
      <MaxEnvelopes>1</MaxEnvelopes>
      <CurrentTime>2026-05-13T08:00:00Z</CurrentTime>
      <RetryCount>0</RetryCount>
      <ParameterList></ParameterList>
    </cwmp:Inform>
  </soapenv:Body>
</soapenv:Envelope>`)

	got := NormalizeCWMP(in)
	gotStr := string(got)
	for _, want := range []string{
		`<cwmp:ID soapenv:mustUnderstand="1">{ID}</cwmp:ID>`,
		`<CurrentTime>{TIMESTAMP}</CurrentTime>`,
		`<Manufacturer>ACME</Manufacturer>`,
		`<EventCode>0 BOOTSTRAP</EventCode>`,
		`<RetryCount>0</RetryCount>`,
		`<MaxEnvelopes>1</MaxEnvelopes>`,
	} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("expected %q in normalized output:\n%s", want, gotStr)
		}
	}
	if strings.Contains(gotStr, `<cwmp:ID soapenv:mustUnderstand="1">17</cwmp:ID>`) {
		t.Errorf("ID 17 should have been replaced")
	}
	if strings.Contains(gotStr, `<CurrentTime>2026-05-13`) {
		t.Errorf("CurrentTime should have been replaced")
	}
}
