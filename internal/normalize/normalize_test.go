package normalize

import (
	"bufio"
	"os"
	"testing"
	"time"
)

var fixedNow = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

func TestNormalizeSpikeStream(t *testing.T) {
	f, err := os.Open("testdata/stream.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var types []string
	var called, finished int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		evs, err := Line(sc.Bytes(), fixedNow)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range evs {
			types = append(types, e.Type)
			switch e.Type {
			case "tool.called":
				called++
			case "tool.finished":
				finished++
			case "run.started":
				if e.SessionId != "d6946aaf-ee92-4104-9191-e1f999b419ec" {
					t.Errorf("session id = %q", e.SessionId)
				}
			case "usage":
				if e.Usage.CostUsd != 0.0355974 || e.Usage.OutputTokens != 596 || e.Usage.CacheReadTokens != 45654 || e.Usage.CacheWriteTokens != 6265 {
					t.Errorf("usage = %v", e.Usage)
				}
			case "run.finished":
				if e.Status != "success" || e.DurationMs != 8350 || e.Text == "" {
					t.Errorf("finished = %v", e)
				}
			}
			if !e.At.AsTime().Equal(fixedNow()) {
				t.Errorf("At not injected for %s", e.Type)
			}
		}
	}
	if len(types) < 3 || types[0] != "run.started" || types[len(types)-2] != "usage" || types[len(types)-1] != "run.finished" {
		t.Fatalf("types = %v", types)
	}
	if called == 0 || called != finished {
		t.Fatalf("called=%d finished=%d", called, finished)
	}
}

func TestNormalizeEditEmitsFileChanged(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Edit","input":{"file_path":"/work/repo/README.md","old_string":"a","new_string":"b"}}]}}`
	evs, err := Line([]byte(raw), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Type != "tool.called" || evs[1].Type != "file.changed" || evs[1].Path != "/work/repo/README.md" {
		t.Fatalf("events = %v", evs)
	}
}

func TestNormalizeIgnoresUnknown(t *testing.T) {
	evs, err := Line([]byte(`{"type":"rate_limit_event"}`), fixedNow)
	if err != nil || len(evs) != 0 {
		t.Fatalf("evs=%v err=%v", evs, err)
	}
}

func TestNormalizeErrorResult(t *testing.T) {
	evs, err := Line([]byte(`{"type":"result","is_error":true,"result":"boom","duration_ms":5}`), fixedNow)
	if err != nil || len(evs) != 2 || evs[1].Status != "error" {
		t.Fatalf("evs=%v err=%v", evs, err)
	}
}

func TestNormalizeInvalidJSON(t *testing.T) {
	if _, err := Line([]byte(`{nope`), fixedNow); err == nil {
		t.Fatal("expected error")
	}
}
