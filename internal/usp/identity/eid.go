package identity

import (
	"errors"
	"fmt"
	"strings"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
)

type Scheme string

const (
	SchemeOUI   Scheme = "oui"
	SchemeCID   Scheme = "cid"
	SchemePEN   Scheme = "pen"
	SchemeSelf  Scheme = "self"
	SchemeUser  Scheme = "user"
	SchemeOS    Scheme = "os"
	SchemeOps   Scheme = "ops"
	SchemeUUID  Scheme = "uuid"
	SchemeIMEI  Scheme = "imei"
	SchemeProto Scheme = "proto"
	SchemeDoc   Scheme = "doc"
	SchemeFQDN  Scheme = "fqdn"
)

const separator = "::"

var validSchemes = map[Scheme]struct{}{
	SchemeOUI: {}, SchemeCID: {}, SchemePEN: {}, SchemeSelf: {},
	SchemeUser: {}, SchemeOS: {}, SchemeOps: {}, SchemeUUID: {},
	SchemeIMEI: {}, SchemeProto: {}, SchemeDoc: {}, SchemeFQDN: {},
}

func EID(oui, serial string) string {
	return string(SchemeOS) + separator + oui + serial
}

func Build(scheme Scheme, id string) (string, error) {
	eid := string(scheme) + separator + id
	if err := Validate(eid); err != nil {
		return "", err
	}
	return eid, nil
}

func Validate(eid string) error {
	idx := strings.Index(eid, separator)
	if idx < 0 {
		return invalid("missing :: separator")
	}
	scheme := Scheme(eid[:idx])
	id := eid[idx+len(separator):]
	if scheme == "" {
		return invalid("empty scheme half")
	}
	if id == "" {
		return invalid("empty id half")
	}
	if _, ok := validSchemes[scheme]; !ok {
		return invalid(fmt.Sprintf("scheme %q not in TR-369 §2.2 R-ARC.2a allow-list", scheme))
	}
	for _, r := range id {
		if !validIDChar(r) {
			return invalid(fmt.Sprintf("id half contains unsafe character %q", r))
		}
	}
	return nil
}

func validIDChar(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '.' || r == '_' || r == '-' || r == ':':
		return true
	default:
		return false
	}
}

func invalid(reason string) error {
	return &cpeerr.Error{Op: "identity.Validate", Kind: cpeerr.KindInvalidArgument, Err: errors.New(reason)}
}
