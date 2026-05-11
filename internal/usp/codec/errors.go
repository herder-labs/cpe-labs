package codec

import (
	"errors"
	"fmt"

	uspproto "github.com/herder-labs/cpe-labs/internal/usp/codec/proto"
)

var (
	errMsgNil              = errors.New("msg is nil")
	errFromEIDEmpty        = errors.New("from_id is empty")
	errToEIDEmpty          = errors.New("to_id is empty")
	errRecordEmpty         = errors.New("record is empty")
	errNotNoSessionContext = errors.New("only no_session_context records are supported")
)

func errVersionUnsupported(v string) error {
	return fmt.Errorf("record version %q is not supported (expected %q)", v, RecordVersion)
}

func errSecurityUnsupported(s uspproto.Record_PayloadSecurity) error {
	return fmt.Errorf("payload_security %s is not supported (expected PLAINTEXT)", s)
}
