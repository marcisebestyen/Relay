package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Limits for job fields.
const (
	MinPriority       = 0
	MaxPriority       = 100
	MinMaxAttempts    = 1
	MaxMaxAttempts    = 100
	MaxTimeoutSeconds = 86400
	MaxNameLength     = 200
)

var (
	queuePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	handlerPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
)

// ValidationErrors maps a JSON field name to its error message.
type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	// Sorted so the message is the same on every run.
	fields := make([]string, 0, len(v))
	for f := range v {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = f + ": " + v[f]
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

func validateName(name string) (string, bool) {
	if utf8.RuneCountInString(name) > MaxNameLength {
		return fmt.Sprintf("must be at most %d characters", MaxNameLength), false
	}
	return "", true
}

func validateHandler(handler string) (string, bool) {
	if handler == "" {
		return "is required", false
	}
	if !handlerPattern.MatchString(handler) {
		return "must match ^[a-z0-9][a-z0-9._-]{0,127}$", false
	}
	return "", true
}

// ValidateHandlerID checks a handler identifier against the domain rule.
func ValidateHandlerID(id string) error {
	if msg, ok := validateHandler(id); !ok {
		return ValidationErrors{"handler": msg}
	}
	return nil
}

func validateQueue(queue string) (string, bool) {
	if !queuePattern.MatchString(queue) {
		return "must match ^[a-z0-9][a-z0-9._-]{0,62}$", false
	}
	return "", true
}

func validatePriority(p int) (string, bool) {
	if p < MinPriority || p > MaxPriority {
		return fmt.Sprintf("must be between %d and %d", MinPriority, MaxPriority), false
	}
	return "", true
}

func validateMaxAttempts(n int) (string, bool) {
	if n < MinMaxAttempts || n > MaxMaxAttempts {
		return fmt.Sprintf("must be between %d and %d", MinMaxAttempts, MaxMaxAttempts), false
	}
	return "", true
}

func validateTimeoutSeconds(n int) (string, bool) {
	if n < 0 || n > MaxTimeoutSeconds {
		return fmt.Sprintf("must be between 0 and %d (0 means no timeout)", MaxTimeoutSeconds), false
	}
	return "", true
}
