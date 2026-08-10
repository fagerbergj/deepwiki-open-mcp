package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const sampleWikiProjects = `[
  {"owner": "fagerbergj", "repo": "quack", "repo_type": "github", "language": "en"},
  {"owner": "google", "repo": "adk-go", "repo_type": "github", "language": "en"}
]`

// fakeDeepwikiOpen stubs the deepwiki-open endpoints the tools call. It
// counts /api/chat/stream hits so tests can assert the not-indexed guard
// never let a request reach it.
type fakeDeepwikiOpen struct {
	*httptest.Server
	chatStreamCalls int32
}

func newFakeDeepwikiOpen(t *testing.T, indexed bool) *fakeDeepwikiOpen {
	t.Helper()
	f := &fakeDeepwikiOpen{}
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
		atomic.AddInt32(&f.chatStreamCalls, 1)
		_, _ = w.Write([]byte("The vetting judge uses a threshold of 0.7."))
	})
	mux.HandleFunc("/api/wiki/projects", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleWikiProjects))
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Server.Close)
	return f
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
	deepwiki := newFakeDeepwikiOpen(t, true)
	cs := connectClient(t, deepwiki.URL)

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"read_wiki_structure", "read_wiki_contents", "ask_question", "list_wikis"} {
		if !got[want] {
			t.Errorf("tools/list missing %q; got %v", want, got)
		}
	}
}

func TestReadWikiStructureTool_HappyPath(t *testing.T) {
	deepwiki := newFakeDeepwikiOpen(t, true)
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
	deepwiki := newFakeDeepwikiOpen(t, true)
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
	deepwiki := newFakeDeepwikiOpen(t, true)
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
	deepwiki := newFakeDeepwikiOpen(t, false)
	cs := connectClient(t, deepwiki.URL)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read_wiki_structure",
		Arguments: map[string]any{"repoName": "fagerbergj/nope"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// Matches Devin's own ergonomics: guidance text, not a protocol error.
	if res.IsError {
		t.Fatalf("IsError = true, want false for an unindexed repo; content = %v", textOf(res))
	}
	if got := textOf(res); !strings.Contains(got, "not indexed") || !strings.Contains(got, "list_wikis") {
		t.Errorf("content = %q, want it to explain the repo is not indexed and point at list_wikis", got)
	}
}

func TestAskQuestionTool_NotIndexed(t *testing.T) {
	deepwiki := newFakeDeepwikiOpen(t, false)
	cs := connectClient(t, deepwiki.URL)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ask_question",
		Arguments: map[string]any{"repoName": "google/adk", "question": "What does this do?"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true, want false for an unindexed repo; content = %v", textOf(res))
	}
	if got := textOf(res); !strings.Contains(got, "not indexed") || !strings.Contains(got, "list_wikis") {
		t.Errorf("content = %q, want it to explain the repo is not indexed and point at list_wikis", got)
	}
	if calls := atomic.LoadInt32(&deepwiki.chatStreamCalls); calls != 0 {
		t.Errorf("chat/stream calls = %d, want 0 - an unindexed repo must never reach chat/stream", calls)
	}
}

func TestListWikisTool_HappyPath(t *testing.T) {
	deepwiki := newFakeDeepwikiOpen(t, true)
	cs := connectClient(t, deepwiki.URL)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_wikis",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("IsError = true, content = %v", textOf(res))
	}
	got := textOf(res)
	for _, want := range []string{"fagerbergj/quack", "google/adk-go"} {
		if !strings.Contains(got, want) {
			t.Errorf("content = %q, missing %q", got, want)
		}
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
