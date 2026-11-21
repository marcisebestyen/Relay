package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/config"
)

func TestParseDotEnv(t *testing.T) {
	in := `# comment

A=1
export B = two
C="quoted value"
D='single'
E=
F=a=b
`
	got, err := config.ParseDotEnv(strings.NewReader(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{"A": "1", "B": "two", "C": "quoted value", "D": "single", "E": "", "F": "a=b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestParseDotEnvRejectsMalformedLine(t *testing.T) {
	_, err := config.ParseDotEnv(strings.NewReader("A=1\nbroken\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want error naming line 2, got %v", err)
	}
}

func TestParseDotEnvRejectsEmptyKey(t *testing.T) {
	if _, err := config.ParseDotEnv(strings.NewReader("=x\n")); err == nil {
		t.Fatal("expected error")
	}
}
