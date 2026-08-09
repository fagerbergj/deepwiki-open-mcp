// Command deepwiki-open-mcp is a thin MCP server exposing a self-hosted
// deepwiki-open instance through the same three tools as Devin's DeepWiki
// MCP server (mcp.deepwiki.com), so any client configured for Devin's server
// works against this one by swapping the URL.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("deepwiki-open-mcp: %v", err)
	}

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("deepwiki-open-mcp listening on %s (deepwiki-open at %s)", addr, cfg.DeepwikiURL)
	if err := http.ListenAndServe(addr, newMux(cfg)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
