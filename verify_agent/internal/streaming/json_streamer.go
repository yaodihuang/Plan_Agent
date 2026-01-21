package streaming

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	assistantPreviewLimit = 500
	promptPreviewLimit    = 4096
)

// JSONStreamer emits NDJSON events that mirror the Codex CLI format.
// When disabled it becomes a no-op, allowing callers to unconditionally
// call the helpers without littering checks throughout the codebase.
type JSONStreamer struct {
	enabled  bool
	writer   io.Writer
	mu       sync.Mutex
	sequence int64
	threadID string
}

func NewJSONStreamer(enabled bool, w io.Writer) *JSONStreamer {
	if !enabled {
		return &JSONStreamer{}
	}
	if w == nil {
		w = os.Stdout
	}
	return &JSONStreamer{
		enabled:  true,
		writer:   w,
		threadID: newThreadID(),
	}
}

func (s *JSONStreamer) Enabled() bool {
	return s != nil && s.enabled
}

func (s *JSONStreamer) ThreadID() string {
	if s == nil {
		return ""
	}
	return s.threadID
}

func (s *JSONStreamer) EmitThreadStarted(task, project, parent string, headless bool) {
	if !s.Enabled() {
		return
	}
	payload := map[string]any{
		"task":             task,
		"project_name":     project,
		"parent_branch_id": parent,
		"headless":         headless,
	}
	s.emit("thread.started", payload)
}

func (s *JSONStreamer) EmitTurnStarted(turnID string, iteration, messageCount, toolCount int) {
	if !s.Enabled() {
		return
	}
	payload := map[string]any{
		"turn_id":       turnID,
		"iteration":     iteration,
		"message_count": messageCount,
		"tool_count":    toolCount,
	}
	s.emit("turn.started", payload)
}

func (s *JSONStreamer) EmitAssistantMessage(turnID, preview string, toolCalls int) {
	if !s.Enabled() {
		return
	}
	snippet := summarize(preview, assistantPreviewLimit)
	payload := map[string]any{
		"turn_id":         turnID,
		"preview":         snippet,
		"tool_call_count": toolCalls,
	}
	if snippet != strings.TrimSpace(preview) {
		payload["truncated"] = true
	}
	s.emit("assistant.message", payload)
}

func (s *JSONStreamer) EmitTurnCompleted(turnID string, iteration, toolCalls int, hasFinal bool) {
	if !s.Enabled() {
		return
	}
	payload := map[string]any{
		"turn_id":          turnID,
		"iteration":        iteration,
		"tool_call_count":  toolCalls,
		"has_final_report": hasFinal,
	}
	s.emit("turn.completed", payload)
}

func (s *JSONStreamer) EmitItemStarted(itemID, kind, name string, args map[string]any) {
	if !s.Enabled() {
		return
	}
	if args == nil {
		args = map[string]any{}
	}
	payload := map[string]any{
		"item_id": itemID,
		"kind":    kind,
		"name":    name,
		"args":    args,
	}
	s.emit("item.started", payload)
}

func (s *JSONStreamer) EmitItemCompleted(itemID, status string, duration time.Duration, branchID, summary string) {
	if !s.Enabled() {
		return
	}
	payload := map[string]any{
		"item_id":     itemID,
		"status":      status,
		"duration_ms": duration.Milliseconds(),
	}
	if branchID != "" {
		payload["branch_id"] = branchID
	}
	if summary != "" {
		payload["summary"] = summarize(summary, assistantPreviewLimit)
	}
	s.emit("item.completed", payload)
}

func (s *JSONStreamer) EmitThreadCompleted(status, summary string, finalReport map[string]any) {
	if !s.Enabled() {
		return
	}
	payload := map[string]any{
		"status": status,
	}
	if summary != "" {
		payload["summary"] = summarize(summary, assistantPreviewLimit)
	}
	if finalReport != nil {
		payload["final_report"] = finalReport
	}
	s.emit("thread.completed", payload)
}

func (s *JSONStreamer) EmitError(scope, message string, extra map[string]any) {
	if !s.Enabled() {
		return
	}
	payload := map[string]any{
		"scope":   scope,
		"message": message,
	}
	for k, v := range extra {
		payload[k] = v
	}
	s.emit("error", payload)
}

func (s *JSONStreamer) emit(eventType string, payload map[string]any) {
	if !s.Enabled() {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++

	envelope := make(map[string]any, len(payload)+4)
	for k, v := range payload {
		envelope[k] = v
	}
	envelope["type"] = eventType
	envelope["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	envelope["sequence"] = s.sequence
	envelope["thread_id"] = s.threadID

	data, err := json.Marshal(envelope)
	if err != nil {
		fmt.Fprintf(os.Stderr, "json_streamer: marshal error: %v\n", err)
		return
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if _, err := s.writer.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "json_streamer: write error: %v\n", err)
	}
}

func summarize(s string, limit int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}

func PromptPreview(prompt string) string {
	return summarize(prompt, promptPreviewLimit)
}

func newThreadID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("thread-%d", time.Now().UnixNano())
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%02x%02x%02x%02x%02x%02x",
		binary.BigEndian.Uint32(buf[0:4]),
		binary.BigEndian.Uint16(buf[4:6]),
		binary.BigEndian.Uint16(buf[6:8]),
		binary.BigEndian.Uint16(buf[8:10]),
		buf[10], buf[11], buf[12], buf[13], buf[14], buf[15],
	)
}

