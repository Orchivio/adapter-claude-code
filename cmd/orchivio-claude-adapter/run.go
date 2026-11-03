package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/soemii/orchivio-adapter-claude-code/internal/normalize"
	pluginv1 "github.com/soemii/orchivio-server/contract/gen/orchivio/plugin/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// lockedWriter serializes stderr writes from claude's own stderr and the stream copy.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

const maxStreamLine = 16 << 20

type runConfig struct {
	ClaudeBin    string
	QuestionPath string
	Now          func() time.Time
	MCPDir       string
	MaxLine      int // max stream line bytes; 0 = maxStreamLine
}

// buildArgs assembles the claude CLI arguments; the first workdir is the cwd, not an argument.
func buildArgs(spec *pluginv1.RunSpec, mcpConfigPath string) []string {
	args := []string{"-p", spec.Prompt, "--output-format", "stream-json", "--verbose",
		"--permission-mode", "bypassPermissions", "--model", spec.Model}
	if spec.Instructions != "" {
		args = append(args, "--append-system-prompt", spec.Instructions)
	}
	args = append(args, "--mcp-config", mcpConfigPath)
	if spec.ResumeSessionId != "" {
		args = append(args, "--resume", spec.ResumeSessionId)
	}
	if len(spec.Workdirs) > 1 {
		for _, d := range spec.Workdirs[1:] {
			args = append(args, "--add-dir", d)
		}
	}
	return args
}

func run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, cfg runConfig) error {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read run spec: %w", err)
	}
	spec := &pluginv1.RunSpec{}
	if err := protojson.Unmarshal(raw, spec); err != nil {
		return fmt.Errorf("parse run spec: %w", err)
	}
	if spec.Prompt == "" {
		return errors.New("prompt required")
	}
	mcpPath, err := writeMCPConfig(cfg.MCPDir)
	if err != nil {
		return err
	}
	defer os.Remove(mcpPath)
	if err := os.MkdirAll(filepath.Dir(cfg.QuestionPath), 0o755); err != nil {
		return fmt.Errorf("create question dir: %w", err)
	}
	if err := os.Remove(cfg.QuestionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale question: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.ClaudeBin, buildArgs(spec, mcpPath)...)
	if len(spec.Workdirs) > 0 {
		cmd.Dir = spec.Workdirs[0]
	}
	stderr = &lockedWriter{w: stderr}
	cmd.Stderr = stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("claude stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	finished, streamErr := pump(pipe, stdout, stderr, cfg)
	if streamErr != nil {
		cancel() // claude would otherwise keep running and block Wait
	}
	waitErr := cmd.Wait()
	if streamErr != nil {
		return streamErr
	}
	if finished {
		return nil
	}
	// claude ended without a result line: still report a terminal event.
	if err := emitQuestion(stdout, cfg); err != nil {
		return err
	}
	return emit(stdout, &pluginv1.AgentEvent{
		Type: "run.finished", At: timestamppb.New(cfg.Now()), Status: "error",
		Text: fmt.Sprintf("claude exited without result (exit code %d)", exitCode(waitErr)),
	})
}

func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 0
}

// pump translates the CLI stream and reports whether run.finished was emitted.
func pump(r io.Reader, stdout, stderr io.Writer, cfg runConfig) (bool, error) {
	sc := bufio.NewScanner(r)
	limit := cfg.MaxLine
	if limit == 0 {
		limit = maxStreamLine
	}
	sc.Buffer(make([]byte, min(limit, 64<<10)), limit)
	finished := false
	for sc.Scan() {
		_, _ = stderr.Write(append(append([]byte{}, sc.Bytes()...), '\n'))
		events, err := normalize.Line(sc.Bytes(), cfg.Now)
		if err != nil {
			continue // unparsable CLI noise stays visible on stderr only
		}
		for _, e := range events {
			if e.Type == "run.finished" {
				finished = true
				if err := emitQuestion(stdout, cfg); err != nil {
					return finished, err
				}
			}
			if err := emit(stdout, e); err != nil {
				return finished, err
			}
		}
	}
	return finished, sc.Err()
}

func emitQuestion(w io.Writer, cfg runConfig) error {
	q, err := os.ReadFile(cfg.QuestionPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read question: %w", err)
	}
	if len(bytes.TrimSpace(q)) == 0 {
		return nil
	}
	return emit(w, &pluginv1.AgentEvent{Type: "question.asked", At: timestamppb.New(cfg.Now()), Text: string(q)})
}

func emit(w io.Writer, e *pluginv1.AgentEvent) error {
	b, err := protojson.MarshalOptions{Multiline: false}.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	// protojson output is deliberately unstable in whitespace; re-compact for one-line guarantee.
	var compact json.RawMessage = b
	b, err = json.Marshal(compact)
	if err != nil {
		return fmt.Errorf("compact event: %w", err)
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func writeMCPConfig(dir string) (string, error) {
	cfg := map[string]any{"mcpServers": map[string]any{"orchivio": map[string]any{
		"command": adapterPath, "args": []string{"ask-human-mcp"}}}}
	b, _ := json.Marshal(cfg)
	f, err := os.CreateTemp(dir, "orchivio-mcp-*.json")
	if err != nil {
		return "", fmt.Errorf("create mcp config: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return "", fmt.Errorf("write mcp config: %w", err)
	}
	return f.Name(), nil
}
