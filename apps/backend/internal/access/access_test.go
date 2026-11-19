package access

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/labstack/echo/v5"
)

const audience = "finchie-aud"

// team stands in for https://<team>.cloudflareaccess.com: it publishes a signing key and signs tokens.
type team struct {
	server *httptest.Server
	key    *rsa.PrivateKey
}

func newTeam(t *testing.T) *team {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keys := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cdn-cgi/access/certs", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(keys)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &team{server: server, key: key}
}

func (tm *team) sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func (tm *team) claims(change func(map[string]any)) map[string]any {
	claims := map[string]any{
		"iss":   tm.server.URL,
		"aud":   []string{audience},
		"email": "owner@example.com",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	if change != nil {
		change(claims)
	}
	return claims
}

func TestOnlyRequestsWithAValidAccessTokenGetThrough(t *testing.T) {
	tm := newTeam(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	e := echo.New()
	e.Use(New(t.Context(), tm.server.URL+"/", audience, "").Middleware("/healthz"))
	ok := func(c *echo.Context) error { return c.String(http.StatusOK, "ok") }
	e.GET("/api/statements", ok)
	e.GET("/healthz", ok)

	cases := []struct {
		name   string
		path   string
		token  string
		status int
	}{
		{"valid token", "/api/statements", tm.sign(t, tm.key, tm.claims(nil)), http.StatusOK},
		{"no token", "/api/statements", "", http.StatusUnauthorized},
		{"another application", "/api/statements", tm.sign(t, tm.key, tm.claims(func(c map[string]any) { c["aud"] = []string{"other-app"} })), http.StatusUnauthorized},
		{"another team", "/api/statements", tm.sign(t, tm.key, tm.claims(func(c map[string]any) { c["iss"] = "https://other.cloudflareaccess.com" })), http.StatusUnauthorized},
		{"expired", "/api/statements", tm.sign(t, tm.key, tm.claims(func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() })), http.StatusUnauthorized},
		{"signed with an unknown key", "/api/statements", tm.sign(t, otherKey, tm.claims(nil)), http.StatusUnauthorized},
		{"garbage", "/api/statements", "not-a-jwt", http.StatusUnauthorized},
		{"health check needs none", "/healthz", "", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			if tc.token != "" {
				request.Header.Set(Header, tc.token)
			}
			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, request)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tc.status, recorder.Body)
			}
		})
	}
}

// A service token's JWT has no e-mail address: the mailbox Worker may upload and nothing else.
func TestServiceTokensMayOnlyUpload(t *testing.T) {
	tm := newTeam(t)
	e := echo.New()
	e.Use(New(t.Context(), tm.server.URL, audience, "").Middleware("/healthz"))
	ok := func(c *echo.Context) error { return c.String(http.StatusOK, "ok") }
	e.GET("/api/statements", ok)
	e.GET("/api/documents", ok)
	e.POST("/api/documents", ok)
	e.POST("/api/documents/:id/reparse", ok)

	machine := tm.sign(t, tm.key, tm.claims(func(c map[string]any) {
		delete(c, "email")
		c["common_name"] = "client-id.access"
	}))
	person := tm.sign(t, tm.key, tm.claims(nil))
	cases := []struct {
		name, method, path, token string
		status                    int
	}{
		{"service token uploads", http.MethodPost, "/api/documents", machine, http.StatusOK},
		{"service token lists documents", http.MethodGet, "/api/documents", machine, http.StatusForbidden},
		{"service token reads statements", http.MethodGet, "/api/statements", machine, http.StatusForbidden},
		{"service token re-parses", http.MethodPost, "/api/documents/1/reparse", machine, http.StatusForbidden},
		{"person reads statements", http.MethodGet, "/api/statements", person, http.StatusOK},
		{"person uploads", http.MethodPost, "/api/documents", person, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
			request.Header.Set(Header, tc.token)
			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, request)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tc.status, recorder.Body)
			}
		})
	}
}

// The agents' service token (apps/cli) may read everything and change nothing; any other service token
// still only uploads.
func TestTheAgentsServiceTokenMayOnlyRead(t *testing.T) {
	tm := newTeam(t)
	e := echo.New()
	e.Use(New(t.Context(), tm.server.URL, audience, "agent-id.access").Middleware("/healthz"))
	ok := func(c *echo.Context) error { return c.String(http.StatusOK, "ok") }
	e.GET("/api/statements", ok)
	e.GET("/api/documents/:id/preview/pdf", ok)
	e.POST("/api/documents", ok)
	e.POST("/api/documents/:id/reparse", ok)
	e.PATCH("/api/accounts/:id", ok)
	e.DELETE("/api/documents/:id", ok)

	serviceToken := func(clientID string) string {
		return tm.sign(t, tm.key, tm.claims(func(c map[string]any) {
			delete(c, "email")
			c["common_name"] = clientID
			c["sub"] = ""
		}))
	}
	agent := serviceToken("agent-id.access")
	mailbox := serviceToken("mailbox-id.access")
	cases := []struct {
		name, method, path, token string
		status                    int
	}{
		{"agent reads statements", http.MethodGet, "/api/statements", agent, http.StatusOK},
		{"agent reads a statement's PDF", http.MethodGet, "/api/documents/1/preview/pdf", agent, http.StatusOK},
		{"agent re-parses", http.MethodPost, "/api/documents/1/reparse", agent, http.StatusForbidden},
		{"agent renames an account", http.MethodPatch, "/api/accounts/1", agent, http.StatusForbidden},
		{"agent deletes a document", http.MethodDelete, "/api/documents/1", agent, http.StatusForbidden},
		{"agent uploads, as any service token may", http.MethodPost, "/api/documents", agent, http.StatusOK},
		{"another service token reads statements", http.MethodGet, "/api/statements", mailbox, http.StatusForbidden},
		{"another service token uploads", http.MethodPost, "/api/documents", mailbox, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
			request.Header.Set(Header, tc.token)
			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, request)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tc.status, recorder.Body)
			}
		})
	}
}
