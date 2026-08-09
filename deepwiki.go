// Package main's deepwiki.go talks to a self-hosted deepwiki-open instance
// and shapes its responses into the three DeepWiki MCP tool results.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotIndexed is returned when deepwiki-open has no wiki cached for a repo.
var ErrNotIndexed = errors.New("repo not indexed in this deepwiki-open instance; index it first")

// askTimeout is generous: deepwiki-open may need to load a model before it
// can answer.
const askTimeout = 300 * time.Second

// deepwikiClient calls a self-hosted deepwiki-open instance's frontend API.
type deepwikiClient struct {
	baseURL    string
	provider   string
	model      string
	httpClient *http.Client
}

func newDeepwikiClient(cfg Config) *deepwikiClient {
	return &deepwikiClient{
		baseURL:    strings.TrimRight(cfg.DeepwikiURL, "/"),
		provider:   cfg.Provider,
		model:      cfg.Model,
		httpClient: &http.Client{Timeout: askTimeout},
	}
}

// wikiPage mirrors both wiki_structure.pages[] (content empty, listing only)
// and generated_pages{} (content populated) entries in deepwiki-open's
// wiki_cache response - same shape, different fill state.
type wikiPage struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type wikiStructure struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Pages       []wikiPage `json:"pages"`
}

type wikiCache struct {
	WikiStructure  *wikiStructure      `json:"wiki_structure"`
	GeneratedPages map[string]wikiPage `json:"generated_pages"`
}

// splitRepoName validates the MCP-facing "owner/repo" convention.
func splitRepoName(repoName string) (owner, repo string, err error) {
	parts := strings.SplitN(repoName, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repoName must be in owner/repo format, got %q", repoName)
	}
	return parts[0], parts[1], nil
}

// fetchWikiCache calls deepwiki-open's wiki_cache endpoint. A 200 with a
// literal `null` body (deepwiki-open's own "not indexed" signal) becomes
// ErrNotIndexed; a non-2xx status becomes an error carrying the status code.
func (c *deepwikiClient) fetchWikiCache(ctx context.Context, repoName string) (*wikiCache, error) {
	owner, repo, err := splitRepoName(repoName)
	if err != nil {
		return nil, err
	}

	u := fmt.Sprintf("%s/api/wiki_cache?owner=%s&repo=%s&repo_type=github&language=en",
		c.baseURL, url.QueryEscape(owner), url.QueryEscape(repo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepwiki-open request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading deepwiki-open response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepwiki-open returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, ErrNotIndexed
	}

	var wc wikiCache
	if err := json.Unmarshal(body, &wc); err != nil {
		return nil, fmt.Errorf("decoding deepwiki-open wiki_cache response: %w", err)
	}
	if wc.WikiStructure == nil {
		return nil, ErrNotIndexed
	}
	return &wc, nil
}

// readWikiStructure renders a readable topic listing: wiki title then each
// page's title and id.
func (c *deepwikiClient) readWikiStructure(ctx context.Context, repoName string) (string, error) {
	wc, err := c.fetchWikiCache(ctx, repoName)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", wc.WikiStructure.Title)
	if wc.WikiStructure.Description != "" {
		fmt.Fprintf(&b, "%s\n", wc.WikiStructure.Description)
	}
	b.WriteString("\n")
	for _, p := range wc.WikiStructure.Pages {
		fmt.Fprintf(&b, "- %s (%s)\n", p.Title, p.ID)
	}
	return b.String(), nil
}

// readWikiContents renders every page's markdown content under a per-page
// header. wiki_structure.pages[].content is always empty in practice - the
// real content lives in generated_pages{}, keyed by page id.
func (c *deepwikiClient) readWikiContents(ctx context.Context, repoName string) (string, error) {
	wc, err := c.fetchWikiCache(ctx, repoName)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", wc.WikiStructure.Title)
	for _, p := range wc.WikiStructure.Pages {
		content := p.Content
		if content == "" {
			if gp, ok := wc.GeneratedPages[p.ID]; ok {
				content = gp.Content
			}
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", p.Title, content)
	}
	return b.String(), nil
}

// askQuestion posts to deepwiki-open's chat/stream endpoint and returns the
// full plain-text response body (it streams chunked text, not SSE frames).
func (c *deepwikiClient) askQuestion(ctx context.Context, repoName, question string) (string, error) {
	owner, repo, err := splitRepoName(repoName)
	if err != nil {
		return "", err
	}

	reqBody, err := json.Marshal(map[string]any{
		"repo_url": fmt.Sprintf("https://github.com/%s/%s", owner, repo),
		"type":     "github",
		"language": "en",
		"provider": c.provider,
		"model":    c.model,
		"messages": []map[string]string{{"role": "user", "content": question}},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat/stream", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("deepwiki-open request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading deepwiki-open response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("deepwiki-open returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}
