package purpose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestInputBoundsPreservingEnds(t *testing.T) {
	value := Input("title", []string{strings.Repeat("a", 9000) + "END"})
	runes := []rune(value)
	if len(runes) > maxCodePoints || !strings.HasPrefix(value, "Title: title") || !strings.HasSuffix(value, "END") || !strings.Contains(value, "omitted") {
		t.Fatalf("input not bounded/preserved: len=%d", len(runes))
	}
}

func TestInputExcludesInjectedContext(t *testing.T) {
	for name, message := range map[string]string{
		"agents":        "# AGENTS.md instructions for /synthetic\n<INSTRUCTIONS>private context</INSTRUCTIONS>",
		"agents mixed":  "# AGENTS.md instructions for /synthetic\n<INSTRUCTIONS>private context</INSTRUCTIONS>\n<environment_context>private context</environment_context>\nplease review",
		"skill":         "<skill name=\"report\">private context</skill>",
		"skill content": "<skill_content name=\"report\">private context</skill_content>",
		"environment":   "<environment_context>private context</environment_context>",
		"developer":     "<developer>private context</developer>",
		"system":        "<system>private context</system>",
		"mixed":         "<skill name=\"report\">private context</skill>please review",
		"unclosed":      "please review<environment_context>private context",
		"nested":        "<skill name=\"report\"><skill>private context</skill>private context</skill>please review",
	} {
		t.Run(name, func(t *testing.T) {
			got := Input("title", []string{message, "actual request"})
			if strings.Contains(got, "private context") || strings.Contains(got, "AGENTS.md instructions") {
				t.Fatalf("injected context reached classifier input: %q", got)
			}
			if !strings.Contains(got, "actual request") || strings.Contains(message, "please review") && !strings.Contains(got, "please review") {
				t.Fatalf("user-authored text was lost: %q", got)
			}
		})
	}
}

func TestClassifyChoiceWithFakeServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fake-key" {
			t.Error("missing bearer auth")
		}
		var body struct {
			State     string `json:"state"`
			Model     string `json:"model"`
			Questions map[string]struct {
				Type     string            `json:"type"`
				Criteria map[string]string `json:"criteria"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "jev-latest" || body.Questions["purpose"].Type != "choice" || body.State != "Title: title\nUser: user text" {
			t.Errorf("unexpected request: %+v", body)
		}
		criteria := body.Questions["purpose"].Criteria
		if len(criteria) != len(defaultModes) {
			t.Errorf("choice count = %d, want %d built-in modes", len(criteria), len(defaultModes))
		}
		for _, mode := range defaultModes {
			if got := criteria[mode.ID]; got != mode.Name+": "+mode.Description {
				t.Errorf("choice %q = %q, want built-in definition", mode.ID, got)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":{"purpose":{"type":"choice","choice":"debug"}}}`))
	}))
	defer server.Close()
	result, err := Classify(context.Background(), server.Client(), server.URL, "fake-key", "title", []string{
		"# AGENTS.md instructions for /synthetic\n<INSTRUCTIONS>private context</INSTRUCTIONS>",
		"<environment_context>private context</environment_context>",
		"<skill name=\"report\">private context</skill>user text",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ModeID != "debug" || result.ModeName != "Debug" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !Current(result, "title", []string{"<skill name=\"report\">changed context</skill>user text"}) {
		t.Fatal("fingerprint must use user-authored input, matching the request")
	}
	old := result
	old.InputFingerprint = Fingerprint("title\x00<skill name=\"report\">private context</skill>user text")
	if Current(old, "title", []string{"user text"}) {
		t.Fatal("classification from before context filtering was reused")
	}
}

func TestClassifyRejectsNonLoopbackHTTPBeforeRequest(t *testing.T) {
	called := false
	client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected request")
	})}
	if _, err := Classify(context.Background(), client, "http://example.test/v1/systemone", "secret", "title", []string{"prompt"}); err == nil {
		t.Fatal("non-loopback HTTP URL was accepted")
	}
	if called {
		t.Fatal("HTTP request was sent before URL validation")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestMissingKeyMakesNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unexpected request") }))
	defer server.Close()
	if _, err := Classify(context.Background(), server.Client(), server.URL, "", "", nil); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestCurrentInvalidatesInputTaxonomyAndVersion(t *testing.T) {
	result := Result{InputFingerprint: Fingerprint("title\x00user"), TaxonomyFingerprint: Fingerprint(string(mustJSON(t, defaultModes))), Version: Version}
	if !Current(result, "title", []string{"user"}) {
		t.Fatal("matching classifier inputs should reuse the result")
	}
	if Current(result, "changed title", []string{"user"}) || Current(result, "title", []string{"changed"}) {
		t.Fatal("changed input should invalidate the result")
	}
	taxonomyFingerprint := result.TaxonomyFingerprint
	result.TaxonomyFingerprint = Fingerprint("previous taxonomy")
	if Current(result, "title", []string{"user"}) {
		t.Fatal("changed taxonomy should invalidate the result")
	}
	result.TaxonomyFingerprint = taxonomyFingerprint
	result.Version = "old"
	if Current(result, "title", []string{"user"}) {
		t.Fatal("changed classifier version should invalidate the result")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStoreContainsOnlyResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "classification.json")
	results := map[string]Result{"s": {InputFingerprint: "hash", TaxonomyFingerprint: "tax", Version: Version, ModeID: "debug", ModeName: "Debug"}}
	if err := SaveStore(path, results); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "prompt") {
		t.Fatalf("raw prompt persisted: %s", data)
	}
	loaded, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["s"].ModeID != "debug" {
		t.Fatalf("bad roundtrip: %+v", loaded)
	}
}

func TestSaveStorePreservesConcurrentResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "classification.json")
	const writers = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := SaveStore(path, map[string]Result{fmt.Sprint(i): {ModeID: "debug"}})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != writers {
		t.Fatalf("stored results = %d, want %d", len(loaded), writers)
	}
}

func TestLoadConfigYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("session_purpose:\n  enabled: true\n  api_url: https://example.test/systemone\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !config.SessionPurpose.Enabled || config.SessionPurpose.APIURL != "https://example.test/systemone" {
		t.Fatalf("unexpected config: %+v", config)
	}
}

func TestLoadConfigRejectsUnknownFieldsAndMultipleDocuments(t *testing.T) {
	for name, contents := range map[string]string{
		"unknown field":      "enabled: true\n",
		"custom taxonomy":    "session_purpose:\n  modes:\n    - id: custom\n      name: Custom\n      description: Custom purpose\n",
		"multiple documents": "session_purpose: {}\n---\nsession_purpose: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err == nil {
				t.Fatal("LoadConfig unexpectedly accepted invalid config")
			}
		})
	}
}

func TestLoadConfigAcceptsEmptyFileAsDisabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("  \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil || config.SessionPurpose.Enabled {
		t.Fatalf("empty config = %+v, %v; want disabled config", config, err)
	}
}

func TestInitializeConfigCreatesDisabledDefaultsWithoutOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catsift", "config.yaml")
	if err := InitializeConfig(path); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.SessionPurpose.Enabled || config.SessionPurpose.APIURL != DefaultURL {
		t.Fatalf("unexpected starter config: %+v", config)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "modes:") || strings.Contains(string(data), "id:") {
		t.Fatalf("starter config must not expose editable modes: %s", data)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitializeConfig(path); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second init error=%v, want file exists", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, current) {
		t.Fatal("existing config was overwritten")
	}
}
