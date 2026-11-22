package api_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcisebestyen/relay/relay-backend/internal/api"
)

const testToken = "s3cret-token-value"

func authRouter(token string) (http.Handler, *bytes.Buffer) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := api.NewRouter(api.Deps{
		Logger: logger, Pinger: fakePinger{}, MaxBodyBytes: 16, AuthToken: token,
		Routes: func(r api.Router) {
			ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
			r.Get("/api/v1/things", ok)
			r.Post("/api/v1/things", ok)
			r.Get("/api/v1/events", ok)
			r.Post("/api/v1/events", ok)
			r.Get("/api/v1/openapi.yaml", ok)
			r.Get("/metrics", ok)
		},
	})
	return h, &logs
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func assertUnauthorized(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q", got)
	}
	if got := decodeError(t, rec).Error.Code; got != "unauthorized" {
		t.Errorf("code = %q", got)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("missing X-Request-ID on 401")
	}
}

func TestAuth_Matrix(t *testing.T) {
	routes := []struct {
		name, method, path string
		protected          bool
	}{
		{"protected get", http.MethodGet, "/api/v1/things", true},
		{"protected post", http.MethodPost, "/api/v1/things", true},
		{"spec", http.MethodGet, "/api/v1/openapi.yaml", true},
		{"events", http.MethodGet, "/api/v1/events", true},
		{"health", http.MethodGet, "/health", false},
		{"ready", http.MethodGet, "/ready", false},
		{"metrics", http.MethodGet, "/metrics", false},
	}
	headers := []struct {
		name, value string
		valid       bool
	}{
		{"ok", "Bearer " + testToken, true},
		{"ok lowercase scheme", "bearer " + testToken, true},
		{"missing", "", false},
		{"wrong", "Bearer nope", false},
		{"prefix of token", "Bearer " + testToken[:5], false},
		{"longer than token", "Bearer " + testToken + "x", false},
		{"wrong scheme", "Basic " + testToken, false},
		{"no scheme", testToken, false},
		{"empty bearer", "Bearer ", false},
	}
	for _, tokenSet := range []bool{true, false} {
		token := ""
		if tokenSet {
			token = testToken
		}
		for _, rt := range routes {
			for _, h := range headers {
				name := "tokenSet=" + map[bool]string{true: "yes", false: "no"}[tokenSet] + "/" + rt.name + "/" + h.name
				t.Run(name, func(t *testing.T) {
					router, _ := authRouter(token)
					hdr := map[string]string{}
					if h.value != "" {
						hdr["Authorization"] = h.value
					}
					rec := do(router, rt.method, rt.path, hdr, nil)
					if tokenSet && rt.protected && !h.valid {
						assertUnauthorized(t, rec)
						return
					}
					if rec.Code == http.StatusUnauthorized {
						t.Fatalf("unexpected 401, body %s", rec.Body)
					}
					if rec.Header().Get("WWW-Authenticate") != "" {
						t.Error("WWW-Authenticate set on accepted request")
					}
				})
			}
		}
	}
}

func TestAuth_AccessTokenQuery(t *testing.T) {
	cases := []struct {
		name, method, target, header string
		want                         int
	}{
		{"events with query token", http.MethodGet, "/api/v1/events?access_token=" + testToken, "", http.StatusOK},
		{"events with wrong query token", http.MethodGet, "/api/v1/events?access_token=nope", "", http.StatusUnauthorized},
		{"events with empty query token", http.MethodGet, "/api/v1/events?access_token=", "", http.StatusUnauthorized},
		{"events header still works", http.MethodGet, "/api/v1/events", "Bearer " + testToken, http.StatusOK},
		{"events wrong header right query", http.MethodGet, "/api/v1/events?access_token=" + testToken, "Bearer nope", http.StatusUnauthorized},
		{"events post ignores query", http.MethodPost, "/api/v1/events?access_token=" + testToken, "", http.StatusUnauthorized},
		{"other route ignores query", http.MethodGet, "/api/v1/things?access_token=" + testToken, "", http.StatusUnauthorized},
		{"events subpath ignores query", http.MethodGet, "/api/v1/events/x?access_token=" + testToken, "", http.StatusUnauthorized},
		{"spec ignores query", http.MethodGet, "/api/v1/openapi.yaml?access_token=" + testToken, "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, _ := authRouter(testToken)
			hdr := map[string]string{}
			if tc.header != "" {
				hdr["Authorization"] = tc.header
			}
			rec := do(router, tc.method, tc.target, hdr, nil)
			if tc.want == http.StatusUnauthorized {
				assertUnauthorized(t, rec)
				return
			}
			if rec.Code != tc.want {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestAuth_QueryTokenIgnoredWhenAuthDisabled(t *testing.T) {
	router, _ := authRouter("")
	if rec := do(router, http.MethodGet, "/api/v1/events?access_token=anything", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAuth_UnknownApiRouteNeedsToken(t *testing.T) {
	router, _ := authRouter(testToken)
	assertUnauthorized(t, do(router, http.MethodGet, "/api/v1/nope", nil, nil))
	if rec := do(router, http.MethodGet, "/api/v1/nope", bearer(testToken), nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestAuth_TokenNeverLogged(t *testing.T) {
	targets := []struct{ name, target, header string }{
		{"query ok", "/api/v1/events?access_token=" + testToken, ""},
		{"query wrong", "/api/v1/events?access_token=leaky-wrong-value", ""},
		{"query on non-events route", "/api/v1/things?access_token=" + testToken, ""},
		{"header ok", "/api/v1/things", "Bearer " + testToken},
		{"header wrong", "/api/v1/things", "Bearer leaky-wrong-value"},
	}
	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			router, logs := authRouter(testToken)
			hdr := map[string]string{}
			if tc.header != "" {
				hdr["Authorization"] = tc.header
			}
			do(router, http.MethodGet, tc.target, hdr, nil)
			if logs.Len() == 0 {
				t.Fatal("expected an access log line")
			}
			for _, secret := range []string{testToken, "leaky-wrong-value", "access_token"} {
				if strings.Contains(logs.String(), secret) {
					t.Errorf("log contains %q: %s", secret, logs)
				}
			}
		})
	}
}

func TestLogAuthStatus(t *testing.T) {
	cases := []struct {
		name, token, addr string
		wantMsg           string
		wantWarn          bool
	}{
		{"enabled loopback", testToken, "127.0.0.1:8080", "auth enabled", false},
		{"enabled public", testToken, ":8080", "auth enabled", false},
		{"disabled ipv4 loopback", "", "127.0.0.1:8080", "auth disabled", false},
		{"disabled ipv6 loopback", "", "[::1]:8080", "auth disabled", false},
		{"disabled localhost", "", "localhost:8080", "auth disabled", false},
		{"disabled empty host", "", ":8080", "auth disabled", true},
		{"disabled wildcard v4", "", "0.0.0.0:8080", "auth disabled", true},
		{"disabled wildcard v6", "", "[::]:8080", "auth disabled", true},
		{"disabled public ip", "", "10.1.2.3:8080", "auth disabled", true},
		{"disabled hostname", "", "relay.example.com:8080", "auth disabled", true},
		{"disabled unparsable", "", "garbage", "auth disabled", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			api.LogAuthStatus(slog.New(slog.NewTextHandler(&logs, nil)), tc.token, tc.addr)
			out := logs.String()
			if !strings.Contains(out, tc.wantMsg) {
				t.Errorf("want %q in %s", tc.wantMsg, out)
			}
			if strings.Contains(out, "level=WARN") != tc.wantWarn {
				t.Errorf("wantWarn = %v, log %s", tc.wantWarn, out)
			}
			if tc.token != "" && strings.Contains(out, tc.token) {
				t.Errorf("token leaked: %s", out)
			}
		})
	}
}
