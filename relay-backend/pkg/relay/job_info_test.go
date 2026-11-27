package relay_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/marcisebestyen/relay/relay-backend/pkg/relay"
)

func TestJobInfoFromContextReturnsFalseWhenAbsent(t *testing.T) {
	if _, ok := relay.JobInfoFromContext(context.Background()); ok {
		t.Fatal("want no job info on a bare context")
	}
}

func TestJobInfoRoundTripsThroughContext(t *testing.T) {
	want := relay.JobInfo{
		JobID:         uuid.New(),
		AttemptNumber: 2,
		Queue:         "emails",
		WorkerID:      uuid.New(),
	}
	ctx := relay.WithJobInfo(context.Background(), want)

	got, ok := relay.JobInfoFromContext(ctx)
	if !ok {
		t.Fatal("want job info on context")
	}
	if got != want {
		t.Errorf("job info = %+v, want %+v", got, want)
	}
}
