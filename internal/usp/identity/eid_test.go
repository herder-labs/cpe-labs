package identity

import (
	"errors"
	"testing"

	"github.com/herder-labs/cpe-labs/internal/cpeerr"
)

func TestEIDFormat(t *testing.T) {
	got := EID("AABBCC", "DDEEFF")
	want := "os::AABBCCDDEEFF"
	if got != want {
		t.Fatalf("EID(AABBCC, DDEEFF) = %q want %q", got, want)
	}
}

func TestValidateAcceptsAllSchemes(t *testing.T) {
	cases := []struct {
		eid string
	}{
		{"oui::AABBCC-1"},
		{"cid::ACME"},
		{"pen::32473"},
		{"self::openacs"},
		{"user::alice"},
		{"os::AABBCCDDEEFF"},
		{"ops::node-1"},
		{"uuid::550e8400-e29b-41d4-a716-446655440000"},
		{"imei::490154203237518"},
		{"proto::tr369"},
		{"doc::tr-181-2-15-1"},
		{"fqdn::cpe.example.com"},
	}
	for _, c := range cases {
		t.Run(c.eid, func(t *testing.T) {
			if err := Validate(c.eid); err != nil {
				t.Fatalf("Validate(%q): %v", c.eid, err)
			}
		})
	}
}

func TestValidateRejectsUnknownScheme(t *testing.T) {
	assertInvalid(t, Validate("bogus::x"))
}

func TestValidateRejectsUnsafeChars(t *testing.T) {
	cases := []string{
		"os::contains>greater",
		"os::contains*star",
		"os::contains space",
		"os::contains/slash",
		"os::contains#hash",
		"os::contains?question",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			assertInvalid(t, Validate(c))
		})
	}
}

func TestValidateRejectsMissingSeparator(t *testing.T) {
	assertInvalid(t, Validate("os:no-second-colon"))
	assertInvalid(t, Validate("plain"))
}

func TestValidateRejectsEmptyHalves(t *testing.T) {
	assertInvalid(t, Validate("::id-only"))
	assertInvalid(t, Validate("os::"))
}

func TestValidateAcceptsColonsInIDHalf(t *testing.T) {
	if err := Validate("self::openacs::primary"); err != nil {
		t.Fatalf("self::openacs::primary should be valid: %v", err)
	}
}

func TestBuildRoundTripsThroughValidate(t *testing.T) {
	eid, err := Build(SchemeOS, "AABBCCDDEEFF")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if eid != "os::AABBCCDDEEFF" {
		t.Fatalf("got %q", eid)
	}
	if _, err := Build(SchemeOS, "has space"); err == nil {
		t.Fatalf("Build with unsafe id should have failed")
	}
}

func assertInvalid(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	var ce *cpeerr.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *cpeerr.Error, got %T", err)
	}
	if ce.Kind != cpeerr.KindInvalidArgument {
		t.Fatalf("Kind=%v want KindInvalidArgument", ce.Kind)
	}
}
