package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleWikiCache = `{
  "wiki_structure": {
    "id": "wiki",
    "title": "Quack Developer Wiki",
    "description": "A guide to the Quack repository.",
    "pages": [
      {"id": "getting-started", "title": "Getting Started", "content": ""},
      {"id": "architecture", "title": "Architecture", "content": ""}
    ]
  },
  "generated_pages": {
    "getting-started": {"id": "getting-started", "title": "Getting Started", "content": "Quack is a local agent helper."},
    "architecture": {"id": "architecture", "title": "Architecture", "content": "It runs a DAG of agents."}
  }
}`

func newTestClient(t *testing.T, handler http.HandlerFunc) *deepwikiClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newDeepwikiClient(Config{DeepwikiURL: srv.URL, Provider: "openrouter", Model: "qwen3.6-35b"})
}

func TestReadWikiStructure(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Query().Get("owner"), "fagerbergj"; got != want {
			t.Errorf("owner = %q, want %q", got, want)
		}
		if got, want := r.URL.Query().Get("repo"), "quack"; got != want {
			t.Errorf("repo = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleWikiCache))
	})

	got, err := client.readWikiStructure(context.Background(), "fagerbergj/quack")
	if err != nil {
		t.Fatalf("readWikiStructure() error = %v", err)
	}
	for _, want := range []string{"Quack Developer Wiki", "Getting Started (getting-started)", "Architecture (architecture)"} {
		if !strings.Contains(got, want) {
			t.Errorf("readWikiStructure() = %q, missing %q", got, want)
		}
	}
}

func TestReadWikiContents(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleWikiCache))
	})

	got, err := client.readWikiContents(context.Background(), "fagerbergj/quack")
	if err != nil {
		t.Fatalf("readWikiContents() error = %v", err)
	}
	for _, want := range []string{"## Getting Started", "Quack is a local agent helper.", "## Architecture", "It runs a DAG of agents."} {
		if !strings.Contains(got, want) {
			t.Errorf("readWikiContents() = %q, missing %q", got, want)
		}
	}
}

func TestFetchWikiCache_NotIndexed(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("null"))
	})

	_, err := client.readWikiStructure(context.Background(), "fagerbergj/nope")
	if !errors.Is(err, ErrNotIndexed) {
		t.Fatalf("readWikiStructure() error = %v, want ErrNotIndexed", err)
	}
}

func TestFetchWikiCache_HTTPError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	_, err := client.readWikiStructure(context.Background(), "fagerbergj/quack")
	if err == nil {
		t.Fatal("readWikiStructure() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want it to mention status 500", err)
	}
}

func TestAskQuestion(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/wiki_cache":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(sampleWikiCache))
		case r.Method == http.MethodPost && r.URL.Path == "/api/chat/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("The vetting judge uses a threshold of 0.7."))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got, err := client.askQuestion(context.Background(), "fagerbergj/quack", "What threshold does the vetting judge use?")
	if err != nil {
		t.Fatalf("askQuestion() error = %v", err)
	}
	if want := "0.7"; !strings.Contains(got, want) {
		t.Errorf("askQuestion() = %q, missing %q", got, want)
	}
}

func TestAskQuestion_NotIndexed(t *testing.T) {
	calls := 0
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat/stream" {
			calls++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("null"))
	})

	_, err := client.askQuestion(context.Background(), "fagerbergj/nope", "What is this?")
	if !errors.Is(err, ErrNotIndexed) {
		t.Fatalf("askQuestion() error = %v, want ErrNotIndexed", err)
	}
	if calls != 0 {
		t.Errorf("chat/stream calls = %d, want 0 - an unindexed repo must never reach chat/stream", calls)
	}
}

func TestReadWikiList(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/wiki/projects" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"owner":"fagerbergj","repo":"quack","repo_type":"github","language":"en"},{"owner":"google","repo":"adk-go","repo_type":"github","language":"en"}]`))
	})

	got, err := client.readWikiList(context.Background())
	if err != nil {
		t.Fatalf("readWikiList() error = %v", err)
	}
	for _, want := range []string{"fagerbergj/quack", "google/adk-go"} {
		if !strings.Contains(got, want) {
			t.Errorf("readWikiList() = %q, missing %q", got, want)
		}
	}
}

func TestSplitRepoName_Invalid(t *testing.T) {
	if _, _, err := splitRepoName("not-a-valid-repo-name"); err == nil {
		t.Fatal("splitRepoName() error = nil, want error for missing slash")
	}
}
