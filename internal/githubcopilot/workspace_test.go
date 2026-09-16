package githubcopilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWorkspaceName(t *testing.T) {
	tests := []struct {
		name     string
		document string
		want     string
		wantErr  bool
	}{
		{name: "double quoted", document: "name: \"Review session\"\nuser_named: true\n", want: "Review session"},
		{name: "single quoted", document: "name: 'Review: session'\n", want: "Review: session"},
		{name: "comment", document: "name: Review session # synthetic\n", want: "Review session"},
		{name: "empty", document: "name:\n", want: ""},
		{name: "missing", document: "id: session-a\n", want: ""},
		{name: "block", document: "name: |\n  Review\n  session\nuser_named: true\n", want: "Review\nsession"},
		{name: "invalid quote", document: "name: 'unterminated\n", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseWorkspaceName(test.document)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("name=%q err=%v, want name=%q err=%v", got, err, test.want, test.wantErr)
			}
		})
	}
}

func TestParseWorkspaceProjectMetadata(t *testing.T) {
	metadata, err := parseWorkspaceMetadata("repository: owner/project\ngit_root: /workspace/project\ncwd: /workspace/project/subdir\n")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Repository != "owner/project" || metadata.GitRoot != "/workspace/project" || metadata.CWD != "/workspace/project/subdir" {
		t.Fatalf("workspace metadata = %#v", metadata)
	}
}

func TestReadWorkspaceNameIsBoundedAndReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.yaml")
	data := []byte("name: 'Review session'\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	name, err := readWorkspaceName(path)
	if err != nil || name != "Review session" {
		t.Fatalf("name=%q err=%v", name, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("metadata changed: before=%v after=%v", before, after)
	}

	if err := os.WriteFile(path, []byte("name: "+strings.Repeat("x", maxWorkspaceMetadataBytes)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWorkspaceName(path); err == nil {
		t.Fatal("expected oversized metadata error")
	}
}
