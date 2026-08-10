package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool names, descriptions and input schemas are copied verbatim from Devin's
// live DeepWiki MCP server (mcp.deepwiki.com/mcp, tools/list, 2026-08-09),
// except repoName on ask_question: Devin's accepts a string or an array of up
// to 10 (multi-repo synthesis); deepwiki-open's chat/stream backend only ever
// takes one repo, so it's narrowed to match read_wiki_* below.
//
// list_wikis is a deliberate fourth tool with no Devin equivalent: Devin's
// index covers all of GitHub, but a self-hosted instance only knows a small
// curated set of repos, so discoverability is worth the extra tool.

type repoInput struct {
	RepoName string `json:"repoName" jsonschema:"GitHub repository in owner/repo format (e.g. \"facebook/react\")."`
}

type askQuestionInput struct {
	RepoName string `json:"repoName" jsonschema:"GitHub repository in owner/repo format (e.g. \"facebook/react\")."`
	Question string `json:"question" jsonschema:"The question to ask about the repository."`
}

// registerTools wires the four DeepWiki-compatible tools onto srv.
func registerTools(srv *mcp.Server, client *deepwikiClient) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "read_wiki_structure",
		Description: "Get a list of documentation topics for a GitHub repository.",
	}, structureHandler(client))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "read_wiki_contents",
		Description: "View documentation about a GitHub repository.",
	}, contentsHandler(client))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "ask_question",
		Description: "Ask any question about a GitHub repository and get an AI-powered, context-grounded response.",
	}, askHandler(client))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_wikis",
		Description: "List the repositories indexed on this deepwiki-open instance. Call this when a repo you asked about is not indexed, or to discover what documentation is available.",
	}, listWikisHandler(client))
}

func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// notIndexedResult matches Devin's own ergonomics for an unknown repo: plain
// text content, isError=false, so the model reads it and self-corrects
// instead of retrying blind against a protocol error.
func notIndexedResult(repoName string) *mcp.CallToolResult {
	text := fmt.Sprintf("%s is not indexed on this deepwiki-open instance. Call list_wikis to see available repositories, or index it in deepwiki-open first.", repoName)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// toolResult shapes a client call's outcome for the wire: ErrNotIndexed
// becomes guidance text, any other error stays a real tool error.
func toolResult(repoName, text string, err error) *mcp.CallToolResult {
	if err != nil {
		if errors.Is(err, ErrNotIndexed) {
			return notIndexedResult(repoName)
		}
		return errResult(err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func structureHandler(client *deepwikiClient) mcp.ToolHandlerFor[repoInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args repoInput) (*mcp.CallToolResult, any, error) {
		text, err := client.readWikiStructure(ctx, args.RepoName)
		return toolResult(args.RepoName, text, err), nil, nil
	}
}

func contentsHandler(client *deepwikiClient) mcp.ToolHandlerFor[repoInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args repoInput) (*mcp.CallToolResult, any, error) {
		text, err := client.readWikiContents(ctx, args.RepoName)
		return toolResult(args.RepoName, text, err), nil, nil
	}
}

func askHandler(client *deepwikiClient) mcp.ToolHandlerFor[askQuestionInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args askQuestionInput) (*mcp.CallToolResult, any, error) {
		text, err := client.askQuestion(ctx, args.RepoName, args.Question)
		return toolResult(args.RepoName, text, err), nil, nil
	}
}

func listWikisHandler(client *deepwikiClient) mcp.ToolHandlerFor[struct{}, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		text, err := client.readWikiList(ctx)
		if err != nil {
			return errResult(err), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	}
}

// newMux builds the full HTTP surface: /healthz plus the MCP server at /mcp.
func newMux(cfg Config) http.Handler {
	client := newDeepwikiClient(cfg)
	srv := mcp.NewServer(&mcp.Implementation{Name: "deepwiki-open-mcp", Version: "0.1.0"}, nil)
	registerTools(srv, client)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	return mux
}
