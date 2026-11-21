package config

import (
	"fmt"
	"log/slog"
)

const redacted = "[REDACTED]"

// plain has Config's fields but no methods, so formatting it cannot recurse
// into String or GoString.
type plain Config

func (c Config) redacted() plain {
	if c.Database.URL != "" {
		c.Database.URL = redacted
	}
	if c.Auth.Token != "" {
		c.Auth.Token = redacted
	}
	return plain(c)
}

// String returns the config with secrets masked.
func (c Config) String() string { return fmt.Sprintf("%+v", c.redacted()) }

// GoString makes %#v safe too.
func (c Config) GoString() string { return c.String() }

// LogValue lets slog log the config with secrets masked.
func (c Config) LogValue() slog.Value { return slog.AnyValue(c.redacted()) }
