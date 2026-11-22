package api

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

func TestCursor_RoundTripKeepsMicrosecondsAndID(t *testing.T) {
	in := domain.JobCursor{
		CreatedAt: time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.FixedZone("x", 2*3600)),
		ID:        uuid.MustParse("11111111-2222-3333-4444-555555555555"),
	}

	got, err := decodeCursor(encodeCursor(in))

	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(in.CreatedAt) || got.CreatedAt.Location() != time.UTC || got.ID != in.ID {
		t.Errorf("got %+v, want %+v", got, in)
	}
}

func TestCursor_IsURLSafe(t *testing.T) {
	c := encodeCursor(domain.JobCursor{CreatedAt: time.Now(), ID: uuid.New()})

	if _, err := base64.RawURLEncoding.DecodeString(c); err != nil {
		t.Errorf("cursor %q is not unpadded base64url: %v", c, err)
	}
}

func TestDecodeCursor_RejectsTamperedValues(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	id := "11111111-2222-3333-4444-555555555555"
	tests := map[string]string{
		"empty":            "",
		"not base64":       "!!!",
		"padded base64":    base64.URLEncoding.EncodeToString([]byte("a")),
		"no separator":     enc("2026-03-04T05:06:07Z"),
		"bad time":         enc("yesterday|" + id),
		"bad id":           enc("2026-03-04T05:06:07Z|nope"),
		"extra field":      enc("2026-03-04T05:06:07Z|" + id + "|x"),
		"nil id":           enc("2026-03-04T05:06:07Z|00000000-0000-0000-0000-000000000000"),
		"non utc time":     enc("2026-03-04T05:06:07+02:00|" + id),
		"time only digits": enc("1772600767|" + id),
	}
	for name, c := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := decodeCursor(c)

			if !errors.Is(err, errInvalidCursor) {
				t.Errorf("err = %v, want errInvalidCursor", err)
			}
		})
	}
}
