package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

var testNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

var testDefaults = domain.Defaults{Queue: "default", Priority: 50, MaxAttempts: 3, TimeoutSeconds: 300}

func intPtr(v int) *int { return &v }

func validInput() domain.NewJobInput {
	return domain.NewJobInput{Handler: "demo.echo"}
}

func build(in domain.NewJobInput, d domain.Defaults) (domain.Job, error) {
	return domain.NewJob(uuid.New(), in, d, testNow)
}

func fieldErrors(t *testing.T, err error) domain.ValidationErrors {
	t.Helper()
	var ve domain.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationErrors, got %v", err)
	}
	return ve
}

type fieldCase struct {
	name  string
	set   func(*domain.NewJobInput)
	field string
	valid bool
}

func runFieldCases(t *testing.T, cases []fieldCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.set(&in)
			_, err := build(in, testDefaults)
			if tc.valid {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			ve := fieldErrors(t, err)
			if _, ok := ve[tc.field]; !ok || len(ve) != 1 {
				t.Errorf("want only %q error, got %v", tc.field, ve)
			}
		})
	}
}

func TestPriorityBoundaries(t *testing.T) {
	set := func(v int) func(*domain.NewJobInput) {
		return func(in *domain.NewJobInput) { in.Priority = intPtr(v) }
	}
	runFieldCases(t, []fieldCase{
		{"-1", set(-1), "priority", false},
		{"0", set(0), "priority", true},
		{"100", set(100), "priority", true},
		{"101", set(101), "priority", false},
	})
}

func TestNameBoundaries(t *testing.T) {
	set := func(s string) func(*domain.NewJobInput) {
		return func(in *domain.NewJobInput) { in.Name = s }
	}
	runFieldCases(t, []fieldCase{
		{"empty", set(""), "name", true},
		{"200 chars", set(strings.Repeat("a", 200)), "name", true},
		{"201 chars", set(strings.Repeat("a", 201)), "name", false},
		{"200 multibyte runes", set(strings.Repeat("é", 200)), "name", true},
		{"201 multibyte runes", set(strings.Repeat("é", 201)), "name", false},
	})
}

func TestQueueRules(t *testing.T) {
	set := func(s string) func(*domain.NewJobInput) {
		return func(in *domain.NewJobInput) { in.Queue = s }
	}
	runFieldCases(t, []fieldCase{
		{"63 chars", set(strings.Repeat("a", 63)), "queue", true},
		{"64 chars", set(strings.Repeat("a", 64)), "queue", false},
		{"leading dash", set("-q"), "queue", false},
		{"leading dot", set(".q"), "queue", false},
		{"leading underscore", set("_q"), "queue", false},
		{"inner separators", set("a.b_c-d"), "queue", true},
		{"digit start", set("1q"), "queue", true},
		{"space inside", set("a b"), "queue", false},
		{"slash", set("a/b"), "queue", false},
		{"uppercase normalized", set("Emails"), "queue", true},
	})
}

func TestHandlerRules(t *testing.T) {
	set := func(s string) func(*domain.NewJobInput) {
		return func(in *domain.NewJobInput) { in.Handler = s }
	}
	runFieldCases(t, []fieldCase{
		{"empty", set(""), "handler", false},
		{"128 chars", set(strings.Repeat("a", 128)), "handler", true},
		{"129 chars", set(strings.Repeat("a", 129)), "handler", false},
		{"leading dash", set("-h"), "handler", false},
		{"leading dot", set(".h"), "handler", false},
		{"uppercase rejected", set("Demo.Echo"), "handler", false},
		{"padded rejected", set(" demo.echo "), "handler", false},
		{"dotted", set("demo.echo-v2_x"), "handler", true},
	})
}

func TestMaxAttemptsBoundaries(t *testing.T) {
	set := func(v int) func(*domain.NewJobInput) {
		return func(in *domain.NewJobInput) { in.MaxAttempts = intPtr(v) }
	}
	runFieldCases(t, []fieldCase{
		{"0", set(0), "max_attempts", false},
		{"1", set(1), "max_attempts", true},
		{"100", set(100), "max_attempts", true},
		{"101", set(101), "max_attempts", false},
	})
}

func TestTimeoutBoundaries(t *testing.T) {
	set := func(v int) func(*domain.NewJobInput) {
		return func(in *domain.NewJobInput) { in.TimeoutSeconds = intPtr(v) }
	}
	runFieldCases(t, []fieldCase{
		{"-1", set(-1), "timeout_seconds", false},
		{"0 means none", set(0), "timeout_seconds", true},
		{"86400", set(86400), "timeout_seconds", true},
		{"86401", set(86401), "timeout_seconds", false},
	})
}

func TestValidationErrorsAreAggregated(t *testing.T) {
	_, err := build(domain.NewJobInput{
		Name:           strings.Repeat("a", 201),
		Queue:          "-bad",
		Priority:       intPtr(101),
		MaxAttempts:    intPtr(0),
		TimeoutSeconds: intPtr(-1),
	}, testDefaults)
	ve := fieldErrors(t, err)
	for _, f := range []string{"name", "handler", "queue", "priority", "max_attempts", "timeout_seconds"} {
		if ve[f] == "" {
			t.Errorf("missing error for %q in %v", f, ve)
		}
	}
	if len(ve) != 6 {
		t.Errorf("want 6 errors, got %d: %v", len(ve), ve)
	}
	if ve.Error() == "" {
		t.Error("Error() is empty")
	}
}

func TestBadDefaultsSurface(t *testing.T) {
	_, err := build(validInput(), domain.Defaults{Queue: "Bad Queue", Priority: 200, MaxAttempts: 0, TimeoutSeconds: -5})
	ve := fieldErrors(t, err)
	for _, f := range []string{"queue", "priority", "max_attempts", "timeout_seconds"} {
		if ve[f] == "" {
			t.Errorf("missing error for %q in %v", f, ve)
		}
	}
}

func TestValidationErrorsMessageIsSortedAndStable(t *testing.T) {
	ve := domain.ValidationErrors{"queue": "q bad", "handler": "h bad"}
	want := "validation failed: handler: h bad; queue: q bad"
	if ve.Error() != want {
		t.Errorf("Error() = %q, want %q", ve.Error(), want)
	}
}
