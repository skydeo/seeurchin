package auth

import "testing"

func TestSignVerifyValueRoundTrip(t *testing.T) {
	s := NewSessions([]byte("super-secret-super-secret-32byte"))
	const v = "fingerprint-abc123"

	signed := s.SignValue(v)
	got, ok := s.VerifyValue(signed)
	if !ok || got != v {
		t.Fatalf("round-trip: got %q ok=%v, want %q true", got, ok, v)
	}
}

func TestVerifyValueRejectsTampering(t *testing.T) {
	s := NewSessions([]byte("super-secret-super-secret-32byte"))
	signed := s.SignValue("payload")

	cases := map[string]string{
		"no separator":  "AAAA",
		"flipped body":  "x" + signed,
		"empty":         "",
		"bad signature": signed[:len(signed)-2] + "zz",
	}
	for name, bad := range cases {
		if _, ok := s.VerifyValue(bad); ok {
			t.Errorf("%s: verified a bad value %q", name, bad)
		}
	}

	// A value signed with a different secret must not verify.
	other := NewSessions([]byte("different-secret-different-secret"))
	if _, ok := other.VerifyValue(signed); ok {
		t.Error("value verified under a foreign secret")
	}
}
