package share

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOwnedCursorRoundTrip(t *testing.T) {
	want := ownedCursor{CreatedAt: time.Date(2026, 9, 19, 12, 0, 0, 123, time.UTC), ID: uuid.New()}
	got, err := decodeOwnedCursor(encodeOwnedCursor(want))
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Fatalf("cursor round trip = %+v, want %+v", got, want)
	}
}

func TestOwnedCursorRejectsMalformedValue(t *testing.T) {
	for _, value := range []string{"bad", "e30", encodeOwnedCursor(ownedCursor{})} {
		if _, err := decodeOwnedCursor(value); err == nil {
			t.Fatalf("cursor %q accepted", value)
		}
	}
}
