package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/orchivio/orchestrator/contract/gen/orchivio/plugin/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestBuildArgsMinimal(t *testing.T) {
	got := buildArgs(&pluginv1.RunSpec{Prompt: "hi", Model: "m", Workdirs: []string{"/work/a"}}, "/tmp/mcp.json")
	want := []string{"-p", "hi", "--output-format", "stream-json", "--verbose", "--permission-mode", "bypassPermissions",
		"--model", "m", "--mcp-config", "/tmp/mcp.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestBuildArgsOptional(t *testing.T) {
	got := strings.Join(buildArgs(&pluginv1.RunSpec{Prompt: "hi", Model: "m", Instructions: "be nice",
		ResumeSessionId: "s1", Workdirs: []string{"/work/a", "/work/b", "/work/c"}}, "c.json"), " ")
	for _, want := range []string{"--append-system-prompt be nice", "--resume s1", "--add-dir /work/b", "--add-dir /work/c"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	if strings.Contains(got, "--add-dir /work/a") {
		t.Error("first workdir must not be --add-dir")
	}
}

func TestRunRequiresPrompt(t *testing.T) {
	err := run(context.Background(), strings.NewReader(`{}`), &bytes.Buffer{}, &bytes.Buffer{}, runConfig{ClaudeBin: "/nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "prompt required") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunEmitsNormalizedEvents(t *testing.T) {
	dir := t.TempDir()
	q := filepath.Join(dir, "q", "question")
	t.Setenv("FAKE_CLAUDE_STREAM", "../../internal/normalize/testdata/stream.jsonl")
	// stale question must be removed before start, then re-created by "the agent"
	if err := os.MkdirAll(filepath.Dir(q), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(q, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf 'Why?' > '" + q + "'\nexec ./testdata/fake-claude.sh\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	cfg := runConfig{ClaudeBin: bin, QuestionPath: q, Now: func() time.Time { return time.Unix(1, 0) }, MCPDir: dir}
	if err := run(context.Background(), strings.NewReader(`{"prompt":"x","model":"m"}`), &out, &errOut, cfg); err != nil {
		t.Fatal(err)
	}
	var types []string
	var question string
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var e pluginv1.AgentEvent
		if err := protojson.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("bad line %q: %v", l, err)
		}
		types = append(types, e.Type)
		if e.Type == "question.asked" {
			question = e.Text
		}
	}
	n := len(types)
	if types[0] != "run.started" || types[n-1] != "run.finished" || types[n-2] != "question.asked" || question != "Why?" {
		t.Fatalf("types = %v question=%q", types, question)
	}
	if !strings.Contains(errOut.String(), `"type":"result"`) {
		t.Error("raw stream not forwarded to stderr")
	}
}

func runFake(t *testing.T, script string, maxLine int) (string, error) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cfg := runConfig{ClaudeBin: bin, QuestionPath: filepath.Join(dir, "question"), MCPDir: dir, MaxLine: maxLine,
		Now: func() time.Time { return time.Unix(1, 0) }}
	err := run(context.Background(), strings.NewReader(`{"prompt":"x","model":"m"}`), &out, &bytes.Buffer{}, cfg)
	return out.String(), err
}

func lastEvent(t *testing.T, out string) *pluginv1.AgentEvent {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	e := &pluginv1.AgentEvent{}
	if err := protojson.Unmarshal([]byte(lines[len(lines)-1]), e); err != nil {
		t.Fatalf("bad line: %v", err)
	}
	return e
}

func TestRunWithoutResultEmitsErrorFinished(t *testing.T) {
	const initLine = `echo '{"type":"system","subtype":"init","session_id":"s","model":"m"}'`
	for name, tc := range map[string]struct{ script, want string }{
		"exit0": {initLine, "exit code 0"},
		"exit3": {initLine + "\nexit 3", "exit code 3"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := runFake(t, tc.script, 0)
			if err != nil {
				t.Fatal(err)
			}
			e := lastEvent(t, out)
			if e.Type != "run.finished" || e.Status != "error" || !strings.Contains(e.Text, tc.want) {
				t.Fatalf("last = %v", e)
			}
		})
	}
}

func TestRunWithoutResultEmitsQuestionFirst(t *testing.T) {
	out, err := runFake(t, `printf 'Why?' > "$(dirname "$0")/question"`, 0)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "question.asked") || !strings.Contains(lines[1], "run.finished") {
		t.Fatalf("out = %s", out)
	}
}

func TestRunOversizedLineKillsClaude(t *testing.T) {
	start := time.Now()
	_, err := runFake(t, "head -c 5000 /dev/zero | tr '\\0' a; echo; sleep 30", 1000)
	if err == nil {
		t.Fatal("expected stream error")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("claude was not killed")
	}
}
