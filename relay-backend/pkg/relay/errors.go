package relay

import "errors"

// nonRetryableError marks an error that must not be retried.
type nonRetryableError struct {
	err error
}

func (e *nonRetryableError) Error() string { return e.err.Error() }

func (e *nonRetryableError) Unwrap() error { return e.err }

// NonRetryable marks err as non-retryable, so the job is not retried.
// It returns nil when err is nil.
func NonRetryable(err error) error {
	if err == nil {
		return nil
	}
	return &nonRetryableError{err: err}
}

// IsNonRetryable reports whether err or any error it wraps came from NonRetryable.
// It uses errors.As, so wrapping with fmt.Errorf("%w") keeps the marker.
func IsNonRetryable(err error) bool {
	var nr *nonRetryableError
	return errors.As(err, &nr)
}
