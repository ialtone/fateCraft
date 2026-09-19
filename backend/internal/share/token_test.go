package share

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSignerRoundTripAndRejectsTampering(t *testing.T) {
	signer, err := newSigner(strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	token := signer.sign(id)
	parsed, err := signer.parse(token)
	if err != nil || parsed != id {
		t.Fatalf("parse() = %s, %v", parsed, err)
	}
	for _, invalid := range []string{
		token + "x",
		strings.Replace(token, "v1.", "v2.", 1),
		"v1.invalid.invalid",
		"",
	} {
		if _, err := signer.parse(invalid); err == nil {
			t.Fatalf("accepted invalid token %q", invalid)
		}
	}
}

func TestSignerRequiresIndependentStrength(t *testing.T) {
	if _, err := newSigner("short"); err != ErrSigningUnavailable {
		t.Fatalf("error = %v", err)
	}
}
