package api

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

var errInvalidCursor = errors.New("invalid cursor")

// encodeCursor returns the opaque token for the position after c. The time is
// RFC 3339 in UTC so it keeps the microseconds Postgres stores.
func encodeCursor(c domain.JobCursor) string {
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a token from encodeCursor. Anything else is errInvalidCursor.
func decodeCursor(s string) (domain.JobCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return domain.JobCursor{}, errInvalidCursor
	}
	ts, idStr, ok := strings.Cut(string(raw), "|")
	if !ok || !strings.HasSuffix(ts, "Z") {
		return domain.JobCursor{}, errInvalidCursor
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return domain.JobCursor{}, errInvalidCursor
	}
	id, err := uuid.Parse(idStr)
	if err != nil || id == uuid.Nil {
		return domain.JobCursor{}, errInvalidCursor
	}
	return domain.JobCursor{CreatedAt: at, ID: id}, nil
}
