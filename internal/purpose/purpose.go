// Package purpose provides optional, privacy-preserving session classification.
package purpose

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const Version = "2"
const DefaultURL = "https://api.typesafe.ai/v1/systemone"
const maxCodePoints = 8000

type mode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

var defaultModes = []mode{
	{"create", "Create", "Build or create something new"},
	{"debug", "Debug", "Find and fix a problem"},
	{"learn", "Learn", "Understand or learn a topic"},
	{"review", "Review", "Review or critique work"},
	{"research", "Research", "Investigate or gather information"},
	{"plan", "Plan", "Plan work or make decisions"},
	{"write", "Write", "Draft or edit written content"},
	{"operate", "Operate", "Run commands or manage a system"},
	{"other", "Other", "Purpose does not fit another mode"},
}

type Config struct {
	SessionPurpose SessionPurposeConfig `json:"session_purpose" yaml:"session_purpose"`
}

type SessionPurposeConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	APIURL  string `json:"api_url,omitempty" yaml:"api_url,omitempty"`
}

type Result struct {
	InputFingerprint    string `json:"input_fingerprint"`
	TaxonomyFingerprint string `json:"taxonomy_fingerprint"`
	Version             string `json:"version"`
	ModeID              string `json:"mode_id"`
	ModeName            string `json:"mode_name"`
}

type store struct {
	Results map[string]Result `json:"results"`
}

var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

var contextTagRE = regexp.MustCompile(`(?is)<(/?)(skill|skill_content|environment_context|developer|system|developer_instructions|system_instructions|system-reminder|instructions)(?:\s[^>]*)?>`)

// userAuthoredText removes known history wrappers before any text leaves the process.
// ponytail: Unmarked injected prose requires provider provenance to distinguish it from user text.
func userAuthoredText(text string) string {
	if strings.HasPrefix(strings.TrimSpace(text), "# AGENTS.md instructions for ") {
		start := strings.Index(strings.ToLower(text), "<instructions>")
		if start < 0 {
			return ""
		}
		text = text[start:]
	}
	var result strings.Builder
	var tags []string
	cursor := 0
	for _, match := range contextTagRE.FindAllStringSubmatchIndex(text, -1) {
		name := strings.ToLower(text[match[4]:match[5]])
		if text[match[2]:match[3]] == "/" {
			if len(tags) > 0 && tags[len(tags)-1] == name {
				tags = tags[:len(tags)-1]
				if len(tags) == 0 {
					cursor = match[1]
				}
			}
			continue
		}
		if len(tags) == 0 {
			result.WriteString(text[cursor:match[0]])
		}
		tags = append(tags, name)
	}
	// An unclosed or mismatched wrapper makes the remaining text unsafe to send.
	if len(tags) == 0 {
		result.WriteString(text[cursor:])
	}
	return strings.TrimSpace(result.String())
}

func classificationInput(title string, messages []string) (string, []string) {
	clean := make([]string, 0, len(messages))
	for _, message := range messages {
		if text := userAuthoredText(message); text != "" {
			clean = append(clean, text)
		}
	}
	return userAuthoredText(title), clean
}

func inputFingerprint(title string, messages []string) string {
	title, messages = classificationInput(title, messages)
	return Fingerprint(title + "\x00" + strings.Join(messages, "\x00"))
}

func Input(title string, messages []string) string {
	title, messages = classificationInput(title, messages)
	parts := make([]string, 0, len(messages)+1)
	if title != "" {
		parts = append(parts, "Title: "+title)
	}
	for _, message := range messages {
		if message != "" {
			parts = append(parts, "User: "+message)
		}
	}
	text := strings.Join(parts, "\n")
	points := []rune(text)
	if len(points) <= maxCodePoints {
		return text
	}
	marker := "\n[… omitted …]\n"
	budget := maxCodePoints - utf8.RuneCountInString(marker)
	start := budget / 2
	return string(points[:start]) + marker + string(points[len(points)-(budget-start):])
}

func Fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func Current(result Result, title string, messages []string) bool {
	taxonomy, _ := json.Marshal(defaultModes)
	return result.InputFingerprint == inputFingerprint(title, messages) && result.TaxonomyFingerprint == Fingerprint(string(taxonomy)) && result.Version == Version
}

func Classify(ctx context.Context, client *http.Client, apiURL, key, title string, messages []string) (Result, error) {
	if strings.TrimSpace(key) == "" {
		return Result{}, errors.New("missing API key")
	}
	if apiURL == "" {
		apiURL = DefaultURL
	}
	if err := validateAPIURL(apiURL); err != nil {
		return Result{}, err
	}
	if client == nil {
		client = defaultHTTPClient
	}
	modes := defaultModes
	input := Input(title, messages)
	criteria := make(map[string]string, len(modes))
	for _, mode := range modes {
		criteria[mode.ID] = mode.Name + ": " + mode.Description
	}
	taxonomy, _ := json.Marshal(modes)
	payload := map[string]any{"state": input, "model": "jev-latest", "questions": map[string]any{"purpose": map[string]any{"type": "choice", "instructions": "Which work mode best describes the purpose of this session?", "criteria": criteria}}}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("purpose request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("purpose request returned HTTP %d", resp.StatusCode)
	}
	var decoded struct {
		Answers map[string]struct {
			Choice string `json:"choice"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return Result{}, fmt.Errorf("invalid purpose response")
	}
	choice := decoded.Answers["purpose"].Choice
	var name string
	for _, mode := range modes {
		if mode.ID == choice {
			name = mode.Name
			break
		}
	}
	if name == "" {
		return Result{}, errors.New("purpose response contained unknown choice")
	}
	return Result{InputFingerprint: inputFingerprint(title, messages), TaxonomyFingerprint: Fingerprint(string(taxonomy)), Version: Version, ModeID: choice, ModeName: name}, nil
}

func validateAPIURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("invalid purpose API URL")
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return nil
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if strings.EqualFold(parsed.Scheme, "http") && (strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return errors.New("purpose API URL must use HTTPS; HTTP is allowed only for loopback endpoints")
}

func LoadStore(path string) (map[string]Result, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Result{}, nil
	}
	if err != nil {
		return nil, err
	}
	var value store
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value.Results == nil {
		value.Results = map[string]Result{}
	}
	return value.Results, nil
}

func SaveStore(path string, results map[string]Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	unlock, err := lockStore(path)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := LoadStore(path)
	if err != nil {
		return err
	}
	for key, result := range results {
		current[key] = result
	}
	// Preserve results saved by concurrent processes after our initial load.
	data, err := json.MarshalIndent(store{Results: current}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
