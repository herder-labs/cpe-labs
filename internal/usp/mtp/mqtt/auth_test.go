package mqtt

import "testing"

// TestDerivePassword_KnownVector pins the wire-format byte-for-byte to
// herder's authservice fixture. If this fails the cpe-sim and
// authservice no longer agree on what password belongs to which EID
// and every CPE will get CONNACK 5.
func TestDerivePassword_KnownVector(t *testing.T) {
	got := DerivePassword("dev-platform-secret", "os::AABBCC-0001")
	want := "zJlHNSr3ouwNmmpsJUE1lcNllMhVJtDpMnNPrLQgWf8"
	if got != want {
		t.Fatalf("DerivePassword fixture mismatch:\n got = %q\nwant = %q", got, want)
	}
}

func TestDerivePassword_DistinctEIDsDifferentPasswords(t *testing.T) {
	a := DerivePassword("s", "os::AABBCC-0001")
	b := DerivePassword("s", "os::AABBCC-0002")
	if a == b {
		t.Fatalf("expected distinct passwords for distinct EIDs, both = %q", a)
	}
}

func TestDerivePassword_DistinctSecretsDifferentPasswords(t *testing.T) {
	a := DerivePassword("s1", "os::AABBCC-0001")
	b := DerivePassword("s2", "os::AABBCC-0001")
	if a == b {
		t.Fatalf("expected distinct passwords for distinct secrets, both = %q", a)
	}
}
