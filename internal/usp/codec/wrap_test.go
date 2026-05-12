package codec

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

func TestWrapMessageRoundTrip(t *testing.T) {
	in := buildOnBoardNotify("AABBCC", "GenericGateway", "AABBCCDDEEFF", "1.5")

	bytes, err := WrapMessage(in, "os::AABBCCDDEEFF", "self::openacs")
	if err != nil {
		t.Fatalf("WrapMessage: %v", err)
	}

	record, out, err := UnwrapRecord(bytes)
	if err != nil {
		t.Fatalf("UnwrapRecord: %v", err)
	}

	if record.GetVersion() != RecordVersion {
		t.Fatalf("Version=%q want %q", record.GetVersion(), RecordVersion)
	}
	if record.GetFromId() != "os::AABBCCDDEEFF" {
		t.Fatalf("FromId=%q", record.GetFromId())
	}
	if record.GetToId() != "self::openacs" {
		t.Fatalf("ToId=%q", record.GetToId())
	}
	if record.GetPayloadSecurity() != uspproto.Record_PLAINTEXT {
		t.Fatalf("PayloadSecurity=%v want PLAINTEXT", record.GetPayloadSecurity())
	}

	if !proto.Equal(in, out) {
		t.Fatalf("round-trip mismatch:\nin=%+v\nout=%+v", in, out)
	}
}

func TestWrapMessageRejectsNilMsg(t *testing.T) {
	_, err := WrapMessage(nil, "os::A", "self::b")
	assertInvalidArg(t, err)
}

func TestWrapMessageRejectsEmptyFromEID(t *testing.T) {
	_, err := WrapMessage(buildEmptyNotify(), "", "self::b")
	assertInvalidArg(t, err)
}

func TestWrapMessageRejectsEmptyToEID(t *testing.T) {
	_, err := WrapMessage(buildEmptyNotify(), "os::a", "")
	assertInvalidArg(t, err)
}

func TestUnwrapRecordRejectsEmptyBytes(t *testing.T) {
	_, _, err := UnwrapRecord(nil)
	assertInvalidArg(t, err)
}

func TestUnwrapRecordRejectsMalformedBytes(t *testing.T) {
	_, _, err := UnwrapRecord([]byte{0xff, 0xff, 0xff})
	assertInvalidArg(t, err)
}

func TestUnwrapRecordRejectsWrongVersion(t *testing.T) {
	record := &uspproto.Record{
		Version:         "9.9",
		ToId:            "self::openacs",
		FromId:          "os::A",
		PayloadSecurity: uspproto.Record_PLAINTEXT,
		RecordType: &uspproto.Record_NoSessionContext{
			NoSessionContext: &uspproto.NoSessionContextRecord{Payload: []byte{}},
		},
	}
	bytes, err := proto.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, _, err = UnwrapRecord(bytes)
	assertInvalidArg(t, err)
}

func TestUnwrapRecordRejectsNonPlaintextSecurity(t *testing.T) {
	record := &uspproto.Record{
		Version:         RecordVersion,
		ToId:            "self::openacs",
		FromId:          "os::A",
		PayloadSecurity: uspproto.Record_TLS12,
		RecordType: &uspproto.Record_NoSessionContext{
			NoSessionContext: &uspproto.NoSessionContextRecord{Payload: []byte{}},
		},
	}
	bytes, err := proto.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, _, err = UnwrapRecord(bytes)
	assertInvalidArg(t, err)
}

func TestUnwrapRecordRejectsMissingNoSessionContext(t *testing.T) {
	record := &uspproto.Record{
		Version:         RecordVersion,
		ToId:            "self::openacs",
		FromId:          "os::A",
		PayloadSecurity: uspproto.Record_PLAINTEXT,
	}
	bytes, err := proto.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	_, _, err = UnwrapRecord(bytes)
	assertInvalidArg(t, err)
}

func buildOnBoardNotify(oui, productClass, serial, version string) *uspproto.Msg {
	return &uspproto.Msg{
		Header: &uspproto.Header{
			MsgId:   "test-msg-1",
			MsgType: uspproto.Header_NOTIFY,
		},
		Body: &uspproto.Body{
			MsgBody: &uspproto.Body_Request{
				Request: &uspproto.Request{
					ReqType: &uspproto.Request_Notify{
						Notify: &uspproto.Notify{
							Notification: &uspproto.Notify_OnBoardReq{
								OnBoardReq: &uspproto.Notify_OnBoardRequest{
									Oui:                            oui,
									ProductClass:                   productClass,
									SerialNumber:                   serial,
									AgentSupportedProtocolVersions: version,
								},
							},
						},
					},
				},
			},
		},
	}
}

func buildEmptyNotify() *uspproto.Msg {
	return &uspproto.Msg{Header: &uspproto.Header{MsgId: "x", MsgType: uspproto.Header_NOTIFY}}
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
