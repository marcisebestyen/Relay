package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/internal/domain"
)

func TestNewJobAppliesDefaults(t *testing.T) {
	id := uuid.New()
	job, err := domain.NewJob(id, validInput(), testDefaults, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != id || job.Handler != "demo.echo" {
		t.Errorf("id/handler wrong: %+v", job)
	}
	if job.Status != domain.JobPending {
		t.Errorf("status = %s, want pending", job.Status)
	}
	if job.Queue != "default" || job.Priority != 50 || job.MaxAttempts != 3 || job.TimeoutSeconds != 300 {
		t.Errorf("defaults not applied: %+v", job)
	}
	if !job.ScheduledAt.Equal(testNow) || !job.CreatedAt.Equal(testNow) || !job.UpdatedAt.Equal(testNow) {
		t.Errorf("timestamps wrong: %+v", job)
	}
	if job.CompletedAt != nil || job.LastError != nil {
		t.Errorf("want nil completed_at/last_error: %+v", job)
	}
}

func TestNewJobKeepsExplicitValues(t *testing.T) {
	sched := testNow.Add(time.Hour)
	payload := json.RawMessage(`{"a":1}`)
	job, err := build(domain.NewJobInput{
		Name:           "send mail",
		Handler:        "mail.send",
		Queue:          " Emails ",
		Payload:        payload,
		Priority:       intPtr(0),
		MaxAttempts:    intPtr(7),
		TimeoutSeconds: intPtr(0),
		ScheduledAt:    &sched,
	}, testDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if job.Name != "send mail" || job.Queue != "emails" || string(job.Payload) != `{"a":1}` {
		t.Errorf("fields wrong: %+v", job)
	}
	if job.Priority != 0 || job.MaxAttempts != 7 || job.TimeoutSeconds != 0 {
		t.Errorf("explicit zero values lost: %+v", job)
	}
	if !job.ScheduledAt.Equal(sched) {
		t.Errorf("scheduled_at = %v, want %v", job.ScheduledAt, sched)
	}
}

func TestNewJobQueueNormalization(t *testing.T) {
	cases := map[string]string{" Default ": "default", "": "default", "   ": "default", "EMAILS": "emails"}
	for in, want := range cases {
		job, err := build(domain.NewJobInput{Handler: "h", Queue: in}, testDefaults)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if job.Queue != want {
			t.Errorf("%q -> %q, want %q", in, job.Queue, want)
		}
	}
}

func TestNewJobInvalidReturnsZeroJob(t *testing.T) {
	job, err := build(domain.NewJobInput{}, testDefaults)
	if err == nil {
		t.Fatal("want error")
	}
	if job.ID != uuid.Nil || job.Status != "" {
		t.Errorf("want zero job, got %+v", job)
	}
}
