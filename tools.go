package main

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool names, descriptions and input schemas are copied verbatim from Devin's
// live DeepWiki MCP server (mcp.deepwiki.com/mcp, tools/list, 2026-08-09),
// except repoName on ask_question: Devin's accepts a string or an array of up
// to 10 (multi-repo synthesis); deepwiki-open's chat/stream backend only ever
// takes one repo, so it's narrowed to match read_wiki_* below.

type repoInput struct {
	RepoName string `json:"repoName" jsonschema:"GitHub repository in owner/repo format (e.g. \"facebook/react\")."`
}

type askQuestionInput struct {
	RepoName string `json:"repoName" jsonschema:"GitHub repository in owner/repo format (e.g. \"facebook/react\")."`
	Question string `json:"question" jsonschema:"The question to ask about the repository."`
}

// registerTools wires the three DeepWiki-compatible tools onto srv.
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
}

func errResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

func structureHandler(client *deepwikiClient) mcp.ToolHandlerFor[repoInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args repoInput) (*mcp.CallToolResult, any, error) {
		text, err := client.readWikiStructure(ctx, args.RepoName)
		if err != nil {
			return errResult(err), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	}
}

func contentsHandler(client *deepwikiClient) mcp.ToolHandlerFor[repoInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args repoInput) (*mcp.CallToolResult, any, error) {
		text, err := client.readWikiContents(ctx, args.RepoName)
		if err != nil {
			return errResult(err), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	}
}

func askHandler(client *deepwikiClient) mcp.ToolHandlerFor[askQuestionInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args askQuestionInput) (*mcp.CallToolResult, any, error) {
		text, err := client.askQuestion(ctx, args.RepoName, args.Question)
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
