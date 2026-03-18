package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mmatczuk/jira-mcp/internal/jira"
	"github.com/mmatczuk/jira-mcp/internal/jiramcp"
)

// Injected at build time via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version information and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("version: %s\ncommit:  %s\ndate:    %s\ngo:      %s\n", version, commit, date, runtime.Version())
		return
	}

	client, err := jira.New(jira.Config{
		URL:        requireEnv("JIRA_URL"),
		Email:      requireEnv("JIRA_EMAIL"),
		APIToken:   requireEnv("JIRA_API_TOKEN"),
		MaxRetries: 3,
		BaseDelay:  time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to create JIRA client: %v", err)
	}

	s := jiramcp.NewServer(client)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := s.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "Required environment variable %s is not set\n", key)
		os.Exit(1)
	}
	return v
}
