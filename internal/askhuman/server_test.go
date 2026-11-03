package askhuman

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAskHumanWritesQuestion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "question")
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := NewServer(path).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ask_human", Arguments: map[string]any{"question": "Which db?"}})
	if err != nil || res.IsError {
		t.Fatalf("res=%v err=%v", res, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "Which db?" {
		t.Fatalf("file=%q err=%v", got, err)
	}
	if txt := res.Content[0].(*mcp.TextContent).Text; txt != Reply {
		t.Fatalf("reply=%q", txt)
	}
}

func TestAskHumanRejectsEmptyQuestion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "question")
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := NewServer(path).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ask_human", Arguments: map[string]any{"question": "  \n"}})
	if err == nil && !res.IsError {
		t.Fatal("expected tool error")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("question file must not exist, stat err = %v", statErr)
	}
}
