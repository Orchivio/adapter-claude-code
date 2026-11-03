// Command orchivio-claude-adapter runs Claude Code inside the sandbox and
// emits normalized AgentEvents (protojson lines) on stdout.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/soemii/orchivio-adapter-claude-code/internal/askhuman"
)

const (
	questionPath = "/tmp/orchivio/question"
	adapterPath  = "/usr/local/bin/orchivio-claude-adapter"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: orchivio-claude-adapter run|ask-human-mcp")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = run(ctx, os.Stdin, os.Stdout, os.Stderr, runConfig{
			ClaudeBin: "claude", QuestionPath: questionPath, Now: time.Now, MCPDir: os.TempDir(),
		})
	case "ask-human-mcp":
		err = askhuman.NewServer(questionPath).Run(ctx, &mcp.StdioTransport{})
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		log.Error("adapter failed", "error", err)
		os.Exit(1)
	}
}
