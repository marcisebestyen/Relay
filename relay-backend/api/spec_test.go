package apispec_test

import (
	"context"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	apispec "github.com/marcisebestyen/relay/relay-backend/api"
	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

func load(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(apispec.Spec)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	return doc
}

func TestSpec_IsValid(t *testing.T) {
	if err := load(t).Validate(context.Background()); err != nil {
		t.Fatalf("spec invalid: %v", err)
	}
}

func TestSpec_HasJobPathsAndBearerAuth(t *testing.T) {
	doc := load(t)
	want := map[string][]string{
		"/api/v1/jobs":               {"POST", "GET"},
		"/api/v1/jobs/{id}":          {"GET"},
		"/api/v1/jobs/{id}/attempts": {"GET"},
		"/api/v1/jobs/{id}/events":   {"GET"},
		"/api/v1/openapi.yaml":       {"GET"},
	}
	for path, methods := range want {
		item := doc.Paths.Find(path)
		if item == nil {
			t.Errorf("missing path %s", path)
			continue
		}
		for _, m := range methods {
			if item.GetOperation(m) == nil {
				t.Errorf("missing %s %s", m, path)
			}
		}
	}
	if doc.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Error("missing bearerAuth security scheme")
	}
}

func enumOf(t *testing.T, name string) []string {
	t.Helper()
	ref := load(t).Components.Schemas[name]
	if ref == nil {
		t.Fatalf("missing schema %s", name)
	}
	var out []string
	for _, v := range ref.Value.Enum {
		out = append(out, v.(string))
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSpec_JobStatusMatchesDomain(t *testing.T) {
	want := []string{
		string(domain.JobPending), string(domain.JobRunning), string(domain.JobCompleted),
		string(domain.JobFailed), string(domain.JobCancelled),
	}
	sort.Strings(want)
	if got := enumOf(t, "JobStatus"); !equal(got, want) {
		t.Errorf("JobStatus enum = %v, want %v", got, want)
	}
}

func TestSpec_AttemptStatusMatchesDomain(t *testing.T) {
	want := []string{
		string(domain.AttemptRunning), string(domain.AttemptCompleted), string(domain.AttemptFailed),
		string(domain.AttemptCancelled), string(domain.AttemptTimedOut), string(domain.AttemptLost),
	}
	sort.Strings(want)
	if got := enumOf(t, "AttemptStatus"); !equal(got, want) {
		t.Errorf("AttemptStatus enum = %v, want %v", got, want)
	}
}
