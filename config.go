package main

import (
	"fmt"
	"os"
)

// Config is env-var-only: no config files, no CLI flags.
type Config struct {
	DeepwikiURL string
	Provider    string
	Model       string
	Port        string
}

func loadConfig() (Config, error) {
	deepwikiURL := os.Getenv("DEEPWIKI_URL")
	if deepwikiURL == "" {
		return Config{}, fmt.Errorf("DEEPWIKI_URL is required")
	}

	provider := os.Getenv("DEEPWIKI_PROVIDER")
	if provider == "" {
		provider = "openrouter"
	}
	model := os.Getenv("DEEPWIKI_MODEL")
	if model == "" {
		model = "qwen3.6-35b"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return Config{
		DeepwikiURL: deepwikiURL,
		Provider:    provider,
		Model:       model,
		Port:        port,
	}, nil
}
