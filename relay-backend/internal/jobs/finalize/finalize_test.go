package finalize

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/marcisebestyen/relay/relay-backend/internal/jobs/claim"
)

func TestTruncateText_ShorterThanLimit_ReturnsUnchanged(t *testing.T) {
	if got := truncateText("short", 10); got != "short" {
		t.Fatalf("truncateText = %q, want unchanged", got)
	}
}

func TestTruncateText_LongerThanLimit_CutsToLimit(t *testing.T) {
	got := truncateText(strings.Repeat("a", 100), 10)
	if len(got) != 10 {
		t.Fatalf("len = %d, want 10", len(got))
	}
}

func TestTruncateText_MultiByteAtCut_KeepsValidUTF8(t *testing.T) {
	// "é" is 2 bytes. A limit of 5 would split the 3rd rune, so the cut must back off.
	got := truncateText(strings.Repeat("é", 10), 5)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateText produced invalid UTF-8: %q", got)
	}
	if len(got) > 5 {
		t.Fatalf("len = %d, want <= 5", len(got))
	}
}

func TestParseErrorKind_KnownKinds_Accepted(t *testing.T) {
	for _, k := range []ErrorKind{KindHandlerError, KindNonRetryable, KindPanic, KindMissingHandler} {
		if !k.valid() {
			t.Errorf("kind %q rejected, want accepted", k)
		}
	}
}

func TestParseErrorKind_UnknownKind_Rejected(t *testing.T) {
	if ErrorKind("bogus").valid() {
		t.Fatal("unknown kind accepted")
	}
}

func TestFail_NilError_ReturnsErrorWithoutTouchingDB(t *testing.T) {
	f := &Finalizer{}
	if err := f.Fail(t.Context(), claim.ClaimedJob{}, nil, KindHandlerError); err == nil {
		t.Fatal("Fail(nil err) returned nil, want error")
	}
}

func TestFail_UnknownKind_ReturnsErrorWithoutTouchingDB(t *testing.T) {
	f := &Finalizer{}
	err := f.Fail(t.Context(), claim.ClaimedJob{}, errors.New("boom"), ErrorKind("bogus"))
	if err == nil {
		t.Fatal("Fail(unknown kind) returned nil, want error")
	}
}

func TestBoundResult_UnderLimit_KeepsResult(t *testing.T) {
	in := json.RawMessage(`{"ok":true}`)
	got, note := boundResult(in, 100)
	if string(got) != `{"ok":true}` || note != "" {
		t.Fatalf("boundResult = (%s, %q), want result kept with no note", got, note)
	}
}

func TestBoundResult_OverLimit_DropsResultWithNote(t *testing.T) {
	in := json.RawMessage(`{"big":"` + strings.Repeat("x", 50) + `"}`)
	got, note := boundResult(in, 10)
	if got != nil {
		t.Fatalf("result = %s, want nil (dropped)", got)
	}
	if !strings.Contains(note, "result dropped") {
		t.Fatalf("note = %q, want it to say result dropped", note)
	}
}

func TestBoundResult_EmptyResult_ReturnsNil(t *testing.T) {
	got, note := boundResult(nil, 10)
	if got != nil || note != "" {
		t.Fatalf("boundResult(nil) = (%v, %q), want (nil, \"\")", got, note)
	}
}
