package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
	"github.com/marcisebestyen/relay/relay-backend/internal/persistence/db"
)

const secret = "s3cr3t-password"

func TestOpenUnreachableHostHidesPassword(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := db.Open(ctx, config.Database{URL: "postgres://relay:" + secret + "@127.0.0.1:1/relay?sslmode=disable"})
	if err == nil {
		t.Fatal("expected connection error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaks password: %v", err)
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("error should name the host: %v", err)
	}
}

func TestOpenInvalidURLHidesPassword(t *testing.T) {
	_, err := db.Open(context.Background(), config.Database{URL: "postgres://relay:" + secret + "@host:notaport/relay"})
	if err == nil {
		t.Fatal("expected parse error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaks password: %v", err)
	}
}

func TestOpenEmptyURLFails(t *testing.T) {
	if _, err := db.Open(context.Background(), config.Database{}); err == nil {
		t.Fatal("expected error for empty URL")
	}
}
