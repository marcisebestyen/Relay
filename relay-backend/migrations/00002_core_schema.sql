-- +goose Up
-- Ids of jobs, attempts and workers have no database default: the app makes
-- them (google/uuid) so it knows an id before the insert.

CREATE TABLE queues (
    name       text        PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO queues (name) VALUES ('default');

CREATE TABLE jobs (
    id                  uuid        PRIMARY KEY,
    name                text,
    handler             text        NOT NULL,
    payload             jsonb       NOT NULL,
    queue               text        NOT NULL DEFAULT 'default' REFERENCES queues (name),
    status              text        NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    priority            smallint    NOT NULL DEFAULT 50 CHECK (priority BETWEEN 0 AND 100),
    scheduled_at        timestamptz NOT NULL DEFAULT now(),
    -- attempt_count: every attempt ever made. It gives the next attempt_number
    -- and is never reset.
    attempt_count       integer     NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    -- cycle_attempt_count: attempts in the current automatic retry cycle, compared
    -- with max_attempts. A manual retry resets it to 0.
    cycle_attempt_count integer     NOT NULL DEFAULT 0 CHECK (cycle_attempt_count >= 0),
    max_attempts        integer     NOT NULL CHECK (max_attempts BETWEEN 1 AND 100),
    timeout_seconds     integer     CHECK (timeout_seconds >= 0),
    -- Ownership (fencing). These are set when a worker claims the job.
    -- worker_id has no foreign key to workers: a workers row is not a durable
    -- liveness record and may be missing or stale.
    -- lease_expires_at is when the job counts as abandoned if the worker has not
    -- renewed it. claim_token is a new random value on every claim and is checked
    -- on every owner write, so a stale worker cannot overwrite a job that another
    -- worker has reclaimed.
    worker_id           uuid,
    lease_expires_at    timestamptz,
    claim_token         uuid,
    cancel_requested_at timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    started_at          timestamptz,
    completed_at        timestamptz,
    last_error          text
);

CREATE TABLE job_attempts (
    id             uuid        PRIMARY KEY,
    job_id         uuid        NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    attempt_number integer     NOT NULL CHECK (attempt_number >= 1),
    -- No foreign key to workers, same reason as jobs.worker_id.
    worker_id      uuid        NOT NULL,
    status         text        NOT NULL
                   CHECK (status IN ('running', 'completed', 'failed', 'cancelled', 'timed_out', 'lost')),
    started_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz,
    error          text,
    error_kind     text,
    result         jsonb,
    -- Also serves "attempts of a job in order".
    UNIQUE (job_id, attempt_number)
);

CREATE TABLE workers (
    id                uuid        PRIMARY KEY,
    hostname          text        NOT NULL,
    pid               integer     NOT NULL,
    version           text        NOT NULL,
    queues            text[]      NOT NULL,
    handlers          text[]      NOT NULL,
    concurrency       integer     NOT NULL CHECK (concurrency >= 1),
    started_at        timestamptz NOT NULL DEFAULT now(),
    last_heartbeat_at timestamptz NOT NULL DEFAULT now(),
    stopped_at        timestamptz
);

CREATE TABLE job_events (
    id         bigserial   PRIMARY KEY,
    job_id     uuid        NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    -- Not a foreign key: it is only a hint for readers of the history.
    attempt_id uuid,
    type       text        NOT NULL,
    -- Keep small. No job payloads.
    data       jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Claim query. scheduled_at <= now() stays a filter; see the EXPLAIN test.
CREATE INDEX jobs_claim_idx ON jobs (queue, priority DESC, created_at, id)
    WHERE status = 'pending';

-- Recovery scan for expired leases.
CREATE INDEX jobs_stale_lease_idx ON jobs (lease_expires_at)
    WHERE status = 'running';

CREATE INDEX jobs_status_queue_created_idx ON jobs (status, queue, created_at DESC);
CREATE INDEX jobs_handler_created_idx ON jobs (handler, created_at DESC);
CREATE INDEX jobs_created_idx ON jobs (created_at DESC, id DESC);

CREATE INDEX job_events_job_created_idx ON job_events (job_id, created_at);

-- +goose Down
DROP TABLE job_events;
DROP TABLE job_attempts;
DROP TABLE workers;
DROP TABLE jobs;
DROP TABLE queues;
