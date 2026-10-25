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
	"strings"
	"time"
	"unicode/utf8"
)

const Version = "1"
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

func Input(title string, messages []string) string {
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
	full := title + "\x00" + strings.Join(messages, "\x00")
	return result.InputFingerprint == Fingerprint(full) && result.TaxonomyFingerprint == Fingerprint(string(taxonomy)) && result.Version == Version
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
		client = &http.Client{Timeout: 30 * time.Second}
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
	full := title + "\x00" + strings.Join(messages, "\x00")
	return Result{InputFingerprint: Fingerprint(full), TaxonomyFingerprint: Fingerprint(string(taxonomy)), Version: Version, ModeID: choice, ModeName: name}, nil
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
	data, err := json.MarshalIndent(store{Results: results}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
