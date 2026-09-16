package githubcopilot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xkumiyu/catsift/internal/usage"
)

const DefaultMaxLineBytes = 4 << 20

type Envelope struct {
	ID          string
	ParentID    string
	Timestamp   time.Time
	Type        string
	Data        map[string]any
	SessionID   string
	SessionName string
	Source      usage.SourceRef
}

type DecodeOptions struct {
	MaxLineBytes int
	Warnings     *WarningCollector
	SessionName  string
}

func (o DecodeOptions) maxLineBytes() int {
	if o.MaxLineBytes <= 0 {
		return DefaultMaxLineBytes
	}
	return o.MaxLineBytes
}

// DecodeFile streams known events without retaining the source file in memory.
func DecodeFile(path string, options DecodeOptions, consume func(Envelope)) (err error) {
	if consume == nil {
		consume = func(Envelope) {}
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close %q: %w", path, closeErr)
		}
	}()
	warnings := options.Warnings
	if warnings == nil {
		warnings = &WarningCollector{}
	}
	reader := bufio.NewReaderSize(file, 64<<10)
	lineNo := 0
	for {
		line, tooLarge, readErr := readLine(reader, options.maxLineBytes())
		if len(line) > 0 || readErr == nil || tooLarge {
			lineNo++
			switch {
			case tooLarge:
				warnings.Add("large_line", path, lineNo)
			case len(bytes.TrimSpace(line)) == 0:
				warnings.Add("empty_line", path, lineNo)
			default:
				decodeLine(line, path, lineNo, warnings, options.SessionName, consume)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return readErr
		}
	}
	return nil
}

func decodeLine(line []byte, path string, lineNo int, warnings *WarningCollector, sessionName string, consume func(Envelope)) {
	var raw struct {
		ID        string          `json:"id"`
		ParentID  string          `json:"parentId"`
		Timestamp json.RawMessage `json:"timestamp"`
		Type      string          `json:"type"`
		Data      map[string]any  `json:"data"`
	}
	if err := json.Unmarshal(line, &raw); err != nil {
		warnings.Add("malformed_json", path, lineNo)
		return
	}
	raw.Type = strings.TrimSpace(raw.Type)
	if !knownEventType(raw.Type) {
		warnings.AddType("unknown_type", raw.Type, path, lineNo)
		return
	}
	timestamp, timestampState := parseTimestamp(raw.Timestamp)
	switch timestampState {
	case timestampMissing:
		warnings.Add("missing_timestamp", path, lineNo)
	case timestampInvalid:
		warnings.Add("invalid_timestamp", path, lineNo)
	}
	sessionID := sessionIDFromPath(path)
	source := usage.SourceRef{
		Path:              path,
		Line:              lineNo,
		Source:            usage.SourceCopilot,
		Agent:             "copilot",
		Provider:          "copilot",
		ProviderSessionID: sessionID,
		EventID:           strings.TrimSpace(raw.ID),
	}
	consume(Envelope{ID: strings.TrimSpace(raw.ID), ParentID: strings.TrimSpace(raw.ParentID), Timestamp: timestamp, Type: raw.Type, Data: raw.Data, SessionID: sessionID, SessionName: sessionName, Source: source})
}

type timestampState uint8

const (
	timestampValid timestampState = iota
	timestampMissing
	timestampInvalid
)

func parseTimestamp(raw json.RawMessage) (time.Time, timestampState) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, timestampMissing
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err == nil {
			return parsed, timestampValid
		}
		return time.Time{}, timestampInvalid
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		value, err := number.Float64()
		if err == nil {
			if value > 1e12 {
				return time.UnixMilli(int64(value)), timestampValid
			}
			return time.Unix(int64(value), int64((value-float64(int64(value)))*1e9)), timestampValid
		}
	}
	return time.Time{}, timestampInvalid
}

func readLine(reader *bufio.Reader, max int) ([]byte, bool, error) {
	var line []byte
	tooLarge := false
	for {
		part, err := reader.ReadSlice('\n')
		if !tooLarge {
			if len(line)+len(part) > max {
				tooLarge = true
				line = nil
			} else {
				line = append(line, part...)
			}
		}
		if err == nil {
			return bytes.TrimSuffix(line, []byte{'\n'}), tooLarge, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return bytes.TrimSuffix(line, []byte{'\n'}), tooLarge, err
	}
}

func knownEventType(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "session.start", "session.shutdown", "session.error", "session.info", "session.model_change",
		"session.context_changed",
		"user.message", "assistant.message", "assistant.usage", "assistant.turn_start", "assistant.turn_end",
		"model.captured_assignment_context", "model.message", "model.messages_snapshot",
		"model.model_call_started", "model.model_call_success", "model.response", "model.turn_started", "model.turn_ended",
		"tool.execution_start", "tool.execution_complete", "permission.requested", "permission.completed", "skill.invoked":
		return true
	default:
		return false
	}
}

type WarningCollector struct {
	warnings []usage.Warning
}

func (c *WarningCollector) Add(reason, path string, line int) {
	c.add(usage.Warning{Reason: reason, Path: path, Line: line, Count: 1})
}

func (c *WarningCollector) AddType(reason, eventType, path string, line int) {
	c.add(usage.Warning{Reason: reason, Type: strings.TrimSpace(eventType), Path: path, Line: line, Count: 1})
}

func (c *WarningCollector) AddFile(reason, path string) {
	c.Add(reason, path, 0)
}

func (c *WarningCollector) add(incoming usage.Warning) {
	for i := range c.warnings {
		current := &c.warnings[i]
		if current.Reason == incoming.Reason && current.Type == incoming.Type && current.Path == incoming.Path {
			current.Count++
			if current.Line != incoming.Line {
				current.Line = 0
			}
			return
		}
	}
	c.warnings = append(c.warnings, incoming)
}

func (c *WarningCollector) Warnings() []usage.Warning {
	return append([]usage.Warning(nil), c.warnings...)
}
