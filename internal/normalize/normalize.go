// Package normalize translates Claude Code stream-json lines into AgentEvents.
package normalize

import (
	"encoding/json"
	"fmt"
	"time"

	pluginv1 "github.com/orchivio/orchestrator/contract/gen/orchivio/plugin/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type line struct {
	Type       string  `json:"type"`
	Subtype    string  `json:"subtype"`
	SessionID  string  `json:"session_id"`
	Model      string  `json:"model"`
	Message    message `json:"message"`
	Result     string  `json:"result"`
	IsError    bool    `json:"is_error"`
	DurationMs int64   `json:"duration_ms"`
	CostUSD    float64 `json:"total_cost_usd"`
	Usage      struct {
		Input      int64 `json:"input_tokens"`
		Output     int64 `json:"output_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type message struct {
	Content json.RawMessage `json:"content"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
}

var fileTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// Line converts one raw stream-json line into zero or more events; unknown
// line types yield no events and no error.
func Line(raw []byte, now func() time.Time) ([]*pluginv1.AgentEvent, error) {
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("parse stream line: %w", err)
	}
	at := timestamppb.New(now())
	ev := func(typ string) *pluginv1.AgentEvent { return &pluginv1.AgentEvent{Type: typ, At: at} }

	switch l.Type {
	case "system":
		if l.Subtype != "init" {
			return nil, nil
		}
		e := ev("run.started")
		e.SessionId, e.Model = l.SessionID, l.Model
		return []*pluginv1.AgentEvent{e}, nil
	case "assistant", "user":
		return blocks(l, ev)
	case "result":
		u := ev("usage")
		u.Usage = &pluginv1.Usage{
			InputTokens: l.Usage.Input, OutputTokens: l.Usage.Output,
			CacheReadTokens: l.Usage.CacheRead, CacheWriteTokens: l.Usage.CacheWrite,
			CostUsd: l.CostUSD,
		}
		f := ev("run.finished")
		f.SessionId, f.Text, f.DurationMs, f.Status = l.SessionID, l.Result, l.DurationMs, "success"
		if l.IsError {
			f.Status = "error"
		}
		return []*pluginv1.AgentEvent{u, f}, nil
	}
	return nil, nil
}

func blocks(l line, ev func(string) *pluginv1.AgentEvent) ([]*pluginv1.AgentEvent, error) {
	var bs []block
	if err := json.Unmarshal(l.Message.Content, &bs); err != nil {
		return nil, nil // ponytail: string content (plain user prompt) carries no events
	}
	var out []*pluginv1.AgentEvent
	for _, b := range bs {
		switch {
		case l.Type == "assistant" && b.Type == "tool_use":
			e := ev("tool.called")
			e.ToolId, e.ToolName, e.ToolInputJson = b.ID, b.Name, string(b.Input)
			out = append(out, e)
			if p := filePath(b); p != "" {
				fc := ev("file.changed")
				fc.ToolId, fc.ToolName, fc.Path = b.ID, b.Name, p
				out = append(out, fc)
			}
		case l.Type == "assistant" && b.Type == "text":
			e := ev("message")
			e.Text = b.Text
			out = append(out, e)
		case l.Type == "user" && b.Type == "tool_result":
			e := ev("tool.finished")
			e.ToolId, e.IsError = b.ToolUseID, b.IsError
			out = append(out, e)
		}
	}
	return out, nil
}

func filePath(b block) string {
	if !fileTools[b.Name] {
		return ""
	}
	var in struct {
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal(b.Input, &in)
	return in.FilePath
}
