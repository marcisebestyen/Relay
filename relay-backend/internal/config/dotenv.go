package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ParseDotEnv reads KEY=VALUE lines. Blank lines and # comments are skipped,
// an optional `export ` prefix is allowed, and one pair of matching quotes is
// stripped. Inline comments are not supported.
func ParseDotEnv(r io.Reader) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("line %d: want KEY=VALUE", n)
		}
		out[key] = unquote(strings.TrimSpace(value))
	}
	return out, sc.Err()
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}
