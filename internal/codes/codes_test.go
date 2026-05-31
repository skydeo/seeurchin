package codes

import (
	"strings"
	"testing"
)

func TestGenerateLengthAndAlphabet(t *testing.T) {
	for i := 0; i < 200; i++ {
		code, err := Generate(DefaultLength)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if len(code) != DefaultLength {
			t.Fatalf("len = %d, want %d", len(code), DefaultLength)
		}
		for _, r := range code {
			if !strings.ContainsRune(alphabet, r) {
				t.Fatalf("code %q contains out-of-alphabet rune %q", code, r)
			}
		}
	}
}

func TestGenerateDefaultsLength(t *testing.T) {
	code, err := Generate(0)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(code) != DefaultLength {
		t.Fatalf("len = %d, want %d", len(code), DefaultLength)
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"k7p2qx":      "K7P2QX",
		" K7P2QX ":    "K7P2QX",
		"blue-otter": "B1E0TTER", // L->1, U dropped (not in alphabet), O->0, hyphen dropped
		"o0i1l":      "00111",    // O->0, I->1, L->1
		"K7-P2 Q.X!": "K7P2QX",   // separators and punctuation dropped
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValid(t *testing.T) {
	if !Valid("k7p2qx", 6) {
		t.Error("expected k7p2qx to be valid at length 6")
	}
	if Valid("k7p2q", 6) {
		t.Error("expected short code to be invalid at length 6")
	}
	if !Valid("K7-P2Q-X", 6) {
		t.Error("expected separators to be ignored")
	}
}

func TestGenerateUniqueRetries(t *testing.T) {
	calls := 0
	code, err := GenerateUnique(DefaultLength, 5, func(string) (bool, error) {
		calls++
		return calls < 3, nil // first two are "taken"
	})
	if err != nil {
		t.Fatalf("GenerateUnique: %v", err)
	}
	if code == "" {
		t.Fatal("expected a code")
	}
	if calls != 3 {
		t.Fatalf("exists called %d times, want 3", calls)
	}
}

func TestGenerateUniqueExhausted(t *testing.T) {
	_, err := GenerateUnique(DefaultLength, 3, func(string) (bool, error) {
		return true, nil // always taken
	})
	if err != ErrExhausted {
		t.Fatalf("err = %v, want ErrExhausted", err)
	}
}
