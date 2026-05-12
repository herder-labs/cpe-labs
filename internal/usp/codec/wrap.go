package codec

import (
	"google.golang.org/protobuf/proto"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

const RecordVersion = "1.5"

func WrapMessage(msg *uspproto.Msg, fromEID, toEID string) ([]byte, error) {
	if msg == nil {
		return nil, &cpeerr.Error{Op: "codec.WrapMessage", Kind: cpeerr.KindInvalidArgument, Err: errMsgNil}
	}
	if fromEID == "" {
		return nil, &cpeerr.Error{Op: "codec.WrapMessage", Kind: cpeerr.KindInvalidArgument, Err: errFromEIDEmpty}
	}
	if toEID == "" {
		return nil, &cpeerr.Error{Op: "codec.WrapMessage", Kind: cpeerr.KindInvalidArgument, Err: errToEIDEmpty}
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return nil, &cpeerr.Error{Op: "codec.WrapMessage", Kind: cpeerr.KindInternal, Err: err}
	}
	record := &uspproto.Record{
		Version:         RecordVersion,
		ToId:            toEID,
		FromId:          fromEID,
		PayloadSecurity: uspproto.Record_PLAINTEXT,
		RecordType: &uspproto.Record_NoSessionContext{
			NoSessionContext: &uspproto.NoSessionContextRecord{Payload: payload},
		},
	}
	out, err := proto.Marshal(record)
	if err != nil {
		return nil, &cpeerr.Error{Op: "codec.WrapMessage", Kind: cpeerr.KindInternal, Err: err}
	}
	return out, nil
}

func UnwrapRecord(b []byte) (*uspproto.Record, *uspproto.Msg, error) {
	if len(b) == 0 {
		return nil, nil, &cpeerr.Error{Op: "codec.UnwrapRecord", Kind: cpeerr.KindInvalidArgument, Err: errRecordEmpty}
	}
	record := &uspproto.Record{}
	if err := proto.Unmarshal(b, record); err != nil {
		return nil, nil, &cpeerr.Error{Op: "codec.UnwrapRecord", Kind: cpeerr.KindInvalidArgument, Err: err}
	}
	if record.GetVersion() != RecordVersion {
		return record, nil, &cpeerr.Error{Op: "codec.UnwrapRecord", Kind: cpeerr.KindInvalidArgument, Err: errVersionUnsupported(record.GetVersion())}
	}
	if record.GetPayloadSecurity() != uspproto.Record_PLAINTEXT {
		return record, nil, &cpeerr.Error{Op: "codec.UnwrapRecord", Kind: cpeerr.KindInvalidArgument, Err: errSecurityUnsupported(record.GetPayloadSecurity())}
	}
	nsc := record.GetNoSessionContext()
	if nsc == nil {
		return record, nil, &cpeerr.Error{Op: "codec.UnwrapRecord", Kind: cpeerr.KindInvalidArgument, Err: errNotNoSessionContext}
	}
	msg := &uspproto.Msg{}
	if err := proto.Unmarshal(nsc.GetPayload(), msg); err != nil {
		return record, nil, &cpeerr.Error{Op: "codec.UnwrapRecord", Kind: cpeerr.KindInvalidArgument, Err: err}
	}
	return record, msg, nil
}
