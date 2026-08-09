package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeDeepwikiOpen stubs the two deepwiki-open endpoints the tools call.
func fakeDeepwikiOpen(t *testing.T, indexed bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/wiki_cache", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !indexed {
			_, _ = w.Write([]byte("null"))
			return
		}
		_, _ = w.Write([]byte(sampleWikiCache))
	})
	mux.HandleFunc("/api/chat/stream", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("The vetting judge uses a threshold of 0.7."))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// connectClient wires the real /mcp HTTP handler behind an httptest.Server
// and drives it with a real Streamable HTTP MCP client - exercising all
// three tools through the actual wire protocol, not just Go function calls.
func connectClient(t *testing.T, deepwikiURL string) *mcp.ClientSession {
	t.Helper()
	cfg := Config{DeepwikiURL: deepwikiURL, Provider: "openrouter", Model: "qwen3.6-35b"}
	mcpSrv := httptest.NewServer(newMux(cfg))
	t.Cleanup(mcpSrv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: mcpSrv.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func textOf(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return ""
	}
	return tc.Text
}

func TestTools_ListedWithDevinNames(t *testing.T) {
	deepwiki := fakeDeepwikiOpen(t, true)
	cs := connectClient(t, deepwiki.URL)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"read_wiki_structure", "read_wiki_contents", "ask_question"} {
		if !got[want] {
			t.Errorf("tools/list missing %q; got %v", want, got)
		}
	}
}

func TestReadWikiStructureTool_HappyPath(t *testing.T) {
	deepwiki := fakeDeepwikiOpen(t, true)
	cs := connectClient(t, deepwiki.URL)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read_wiki_structure",
		Arguments: map[string]any{"repoName": "fagerbergj/quack"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true, content = %v", textOf(res))
	}
	if got := textOf(res); !strings.Contains(got, "Getting Started (getting-started)") {
		t.Errorf("content = %q, missing page listing", got)
	}
}

func TestReadWikiContentsTool_HappyPath(t *testing.T) {
	deepwiki := fakeDeepwikiOpen(t, true)
	cs := connectClient(t, deepwiki.URL)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read_wiki_contents",
		Arguments: map[string]any{"repoName": "fagerbergj/quack"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true, content = %v", textOf(res))
	}
	if got := textOf(res); !strings.Contains(got, "Quack is a local agent helper.") {
		t.Errorf("content = %q, missing page content", got)
	}
}

func TestAskQuestionTool_HappyPath(t *testing.T) {
	deepwiki := fakeDeepwikiOpen(t, true)
	cs := connectClient(t, deepwiki.URL)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ask_question",
		Arguments: map[string]any{"repoName": "fagerbergj/quack", "question": "What threshold does the vetting judge use?"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true, content = %v", textOf(res))
	}
	if got := textOf(res); !strings.Contains(got, "0.7") {
		t.Errorf("content = %q, missing answer", got)
	}
}

func TestReadWikiStructureTool_NotIndexed(t *testing.T) {
	deepwiki := fakeDeepwikiOpen(t, false)
	cs := connectClient(t, deepwiki.URL)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read_wiki_structure",
		Arguments: map[string]any{"repoName": "fagerbergj/nope"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("IsError = false, want true for an unindexed repo")
	}
	if got := textOf(res); !strings.Contains(got, "not indexed") {
		t.Errorf("content = %q, want it to explain the repo is not indexed", got)
	}
}

func TestHealthz(t *testing.T) {
	cfg := Config{DeepwikiURL: "http://example.invalid"}
	srv := httptest.NewServer(newMux(cfg))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestLoadConfig_MissingDeepwikiURL(t *testing.T) {
	t.Setenv("DEEPWIKI_URL", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig() error = nil, want error when DEEPWIKI_URL is unset")
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("DEEPWIKI_URL", "http://deepwiki:3000")
	t.Setenv("DEEPWIKI_PROVIDER", "")
	t.Setenv("DEEPWIKI_MODEL", "")
	t.Setenv("PORT", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.Provider != "openrouter" {
		t.Errorf("Provider = %q, want %q", cfg.Provider, "openrouter")
	}
	if cfg.Model != "qwen3.6-35b" {
		t.Errorf("Model = %q, want %q", cfg.Model, "qwen3.6-35b")
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
}
