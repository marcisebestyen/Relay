package domain_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

func validFilter() domain.JobFilter {
	return domain.JobFilter{Sort: domain.SortCreatedAtDesc, Limit: domain.DefaultListLimit}
}

func TestJobFilterValidate_AcceptsValidFilter(t *testing.T) {
	f := validFilter()
	f.Statuses = []domain.JobStatus{domain.JobPending, domain.JobFailed}
	f.Queues = []string{"default", "reports"}
	f.Handler = "demo.echo"
	f.IDPrefix = "1234abcd-12"
	f.Sort = domain.SortCreatedAtAsc
	f.Limit = domain.MaxListLimit

	if err := f.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestJobFilterValidate_RejectsBadFields(t *testing.T) {
	tests := []struct {
		name  string
		set   func(*domain.JobFilter)
		field string
	}{
		{"unknown status", func(f *domain.JobFilter) { f.Statuses = []domain.JobStatus{"pending", "weird"} }, "status"},
		{"empty status", func(f *domain.JobFilter) { f.Statuses = []domain.JobStatus{""} }, "status"},
		{"bad queue", func(f *domain.JobFilter) { f.Queues = []string{"ok", "Bad Queue"} }, "queue"},
		{"bad handler", func(f *domain.JobFilter) { f.Handler = "Bad Handler" }, "handler"},
		{"id prefix not hex", func(f *domain.JobFilter) { f.IDPrefix = "xyz" }, "id_prefix"},
		{"id prefix only dashes", func(f *domain.JobFilter) { f.IDPrefix = "--" }, "id_prefix"},
		{"id prefix too long", func(f *domain.JobFilter) { f.IDPrefix = strings.Repeat("a", 33) }, "id_prefix"},
		{"id prefix with wildcard", func(f *domain.JobFilter) { f.IDPrefix = "ab%" }, "id_prefix"},
		{"unknown sort", func(f *domain.JobFilter) { f.Sort = "priority" }, "sort"},
		{"limit zero", func(f *domain.JobFilter) { f.Limit = 0 }, "limit"},
		{"limit negative", func(f *domain.JobFilter) { f.Limit = -1 }, "limit"},
		{"limit above max", func(f *domain.JobFilter) { f.Limit = domain.MaxListLimit + 1 }, "limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := validFilter()
			tt.set(&f)
			errs := fieldErrors(t, f.Validate())
			if errs[tt.field] == "" {
				t.Errorf("want error for %q, got %v", tt.field, errs)
			}
		})
	}
}

func TestJobFilterValidate_ReportsEveryBadField(t *testing.T) {
	f := domain.JobFilter{Statuses: []domain.JobStatus{"x"}, Sort: "x", Limit: 0}

	errs := fieldErrors(t, f.Validate())

	if len(errs) != 3 {
		t.Errorf("errors = %v, want status, sort and limit", errs)
	}
}

func TestJobFilterIDRange(t *testing.T) {
	full := uuid.MustParse("12345678-1234-5678-9abc-def012345678")
	tests := []struct {
		name   string
		prefix string
		lo, hi string
		ok     bool
	}{
		{"empty prefix has no range", "", "", "", false},
		{"short prefix", "1234", "12340000-0000-0000-0000-000000000000", "1234ffff-ffff-ffff-ffff-ffffffffffff", true},
		{"upper case and dashes", "1234ABCD-12", "1234abcd-1200-0000-0000-000000000000", "1234abcd-12ff-ffff-ffff-ffffffffffff", true},
		{"full uuid is exact", full.String(), full.String(), full.String(), true},
		{"full uuid without dashes is exact", strings.ReplaceAll(full.String(), "-", ""), full.String(), full.String(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lo, hi, ok := domain.JobFilter{IDPrefix: tt.prefix}.IDRange()

			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && (lo.String() != tt.lo || hi.String() != tt.hi) {
				t.Errorf("range = %s..%s, want %s..%s", lo, hi, tt.lo, tt.hi)
			}
		})
	}
}
