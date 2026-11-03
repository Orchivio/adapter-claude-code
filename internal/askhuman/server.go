// Package askhuman provides the ask_human MCP tool: the agent parks a question
// for the ticket and ends its run.
package askhuman

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Reply is what the agent is told after asking.
const Reply = "Die Frage wurde ins Ticket gestellt. Beende jetzt deine Arbeit ohne weitere Schritte. Die Antwort bekommst du in einem neuen Run."

type input struct {
	Question string `json:"question" jsonschema:"the question for the human"`
}

// NewServer builds an MCP server whose ask_human tool writes the question to questionPath.
func NewServer(questionPath string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "orchivio", Version: "0.1.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "ask_human", Description: "Ask the human a question when you cannot proceed. After calling, stop working."},
		func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
			if err := os.MkdirAll(filepath.Dir(questionPath), 0o755); err != nil {
				return nil, nil, fmt.Errorf("create question dir: %w", err)
			}
			if err := os.WriteFile(questionPath, []byte(in.Question), 0o644); err != nil {
				return nil, nil, fmt.Errorf("write question: %w", err)
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: Reply}}}, nil, nil
		})
	return s
}
