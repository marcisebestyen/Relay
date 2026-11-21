package config_test

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func secretCfgEnv() map[string]string {
	return map[string]string{
		"RELAY_DATABASE_URL": "postgres://user:hunter2@host/db",
		"RELAY_API_TOKEN":    "s3cr3t-token",
	}
}

func TestStringRedactsSecrets(t *testing.T) {
	cfg := load(t, secretCfgEnv())
	for name, out := range map[string]string{
		"String":  cfg.String(),
		"%v":      fmt.Sprintf("%v", cfg),
		"%+v":     fmt.Sprintf("%+v", cfg),
		"%#v":     fmt.Sprintf("%#v", cfg),
		"pointer": fmt.Sprintf("%v", &cfg),
	} {
		for _, secret := range []string{"hunter2", "s3cr3t-token", "postgres://"} {
			if strings.Contains(out, secret) {
				t.Errorf("%s leaks %q: %s", name, secret, out)
			}
		}
	}
	if !strings.Contains(cfg.String(), ":8080") {
		t.Errorf("non-secret fields should still show: %s", cfg.String())
	}
}

func TestLogValueRedactsSecrets(t *testing.T) {
	cfg := load(t, secretCfgEnv())
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("cfg", "config", cfg)
	out := buf.String()
	for _, secret := range []string{"hunter2", "s3cr3t-token"} {
		if strings.Contains(out, secret) {
			t.Errorf("log leaks %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "8080") {
		t.Errorf("log should include non-secret fields: %s", out)
	}
}

func TestRedactionMarksUnsetSecretsAsEmpty(t *testing.T) {
	cfg := load(t, nil)
	if strings.Contains(cfg.String(), "[REDACTED]") {
		t.Errorf("unset secrets should not show as redacted: %s", cfg.String())
	}
}
