package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"
	oauth "github.com/tuannvm/oauth-mcp-proxy"
	"github.com/tuannvm/mcp-trino/internal/config"
)

// fakeValidator accepts exactly one token and records what it was asked.
type fakeValidator struct {
	valid string
	seen  []string
}

func (f *fakeValidator) ValidateTokenCached(_ context.Context, token string) (*oauth.User, error) {
	f.seen = append(f.seen, token)
	if token == f.valid {
		return &oauth.User{Subject: "user@example.com"}, nil
	}
	return nil, errors.New("authentication failed: oidc: token is expired")
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`

func newOAuthTestHandler(t *testing.T, v tokenValidator) http.HandlerFunc {
	t.Helper()
	t.Setenv("MCP_URL", "https://mcp.example.com")

	mcpSrv := mcpserver.NewMCPServer("test", "0.0.1", mcpserver.WithToolCapabilities(true))
	streamable := mcpserver.NewStreamableHTTPServer(mcpSrv,
		mcpserver.WithEndpointPath("/mcp"),
		mcpserver.WithHTTPContextFunc(oauth.CreateHTTPContextFunc()),
	)
	s := &Server{
		mcpServer: mcpSrv,
		config:    &config.TrinoConfig{OAuthEnabled: true},
		version:   "test",
		validator: v,
	}
	return s.createMCPHandler(streamable)
}

func postMCP(h http.HandlerFunc, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initializeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func assertUnauthorizedWithDiscovery(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body: %s", rec.Code, rec.Body.String())
	}
	headers := strings.Join(rec.Header().Values("WWW-Authenticate"), "\n")
	if !strings.Contains(headers, `error="invalid_token"`) {
		t.Errorf("WWW-Authenticate missing invalid_token: %q", headers)
	}
	if !strings.Contains(headers, `resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource"`) {
		t.Errorf("WWW-Authenticate missing resource_metadata: %q", headers)
	}
}

// An expired (or otherwise invalid) bearer must get HTTP 401, not a JSON-RPC
// error inside a 200, so MCP clients use their refresh token (DATAENG-3825).
func TestMCPHandler_InvalidBearerReturns401(t *testing.T) {
	v := &fakeValidator{valid: "good"}
	rec := postMCP(newOAuthTestHandler(t, v), "Bearer expired")

	assertUnauthorizedWithDiscovery(t, rec)
	if len(v.seen) != 1 || v.seen[0] != "expired" {
		t.Errorf("validator saw %v, want [expired]", v.seen)
	}
}

func TestMCPHandler_MissingBearerReturns401(t *testing.T) {
	v := &fakeValidator{valid: "good"}
	assertUnauthorizedWithDiscovery(t, postMCP(newOAuthTestHandler(t, v), ""))
	assertUnauthorizedWithDiscovery(t, postMCP(newOAuthTestHandler(t, v), "Basic dXNlcjpwYXNz"))
	if len(v.seen) != 0 {
		t.Errorf("validator called for a request without a bearer: %v", v.seen)
	}
}

func TestMCPHandler_ValidBearerReachesMCPServer(t *testing.T) {
	v := &fakeValidator{valid: "good"}
	rec := postMCP(newOAuthTestHandler(t, v), "Bearer good")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"serverInfo"`) {
		t.Errorf("initialize response missing serverInfo: %s", rec.Body.String())
	}
}

// If OAuth is enabled but its server failed to initialize, requests must be
// rejected instead of reaching the tools unauthenticated.
func TestMCPHandler_OAuthNotInitializedFailsClosed(t *testing.T) {
	rec := postMCP(newOAuthTestHandler(t, nil), "Bearer good")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body: %s", rec.Code, rec.Body.String())
	}
}

func TestMCPHandler_OptionsSkipsAuth(t *testing.T) {
	h := newOAuthTestHandler(t, &fakeValidator{valid: "good"})
	req := httptest.NewRequest(http.MethodOptions, "/mcp", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
