package opencode

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestResolveHomeUsesOpenCodePrecedence(t *testing.T) {
	tests := []struct {
		name string
		args ResolveOptions
		want string
	}{
		{
			name: "explicit",
			args: ResolveOptions{Explicit: "/explicit", EnvHome: "/env", UserHome: "/home/user"},
			want: "/explicit",
		},
		{
			name: "environment",
			args: ResolveOptions{EnvHome: "/env", UserHome: "/home/user"},
			want: "/env",
		},
		{
			name: "linux xdg",
			args: ResolveOptions{UserHome: "/home/user", XDGDataHome: "/xdg/data"},
			want: "/xdg/data/opencode",
		},
		{
			name: "linux default",
			args: ResolveOptions{UserHome: "/home/user"},
			want: "/home/user/.local/share/opencode",
		},
		{
			name: "darwin default",
			args: ResolveOptions{UserHome: "/home/user"},
			want: filepath.Join("/home/user", ".local", "share", "opencode"),
		},
		{
			name: "windows default",
			args: ResolveOptions{UserHome: `C:\Users\user`},
			want: filepath.Join(`C:\Users\user`, ".local", "share", "opencode"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveHomeFrom(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if got != filepath.Clean(tt.want) {
				t.Fatalf("resolved home = %q, want %q", got, tt.want)
			}
		})
	}

	if _, err := ResolveHomeFrom(ResolveOptions{}); err == nil {
		t.Fatal("empty resolver inputs should fail")
	}
}

func TestResolveHomeUsesExplicitValueWithoutUserHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("OPENCODE_HOME", "/environment")
	got, err := ResolveHome("/explicit")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/explicit" {
		t.Fatalf("resolved home = %q, want /explicit", got)
	}
}

func TestResolveHomeUsesXDGDataHomeWithoutUserHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("OPENCODE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "/xdg/data")
	got, err := ResolveHome("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/xdg/data", "opencode")
	if got != want {
		t.Fatalf("resolved home = %q, want %q", got, want)
	}
}

func TestValidateDataRootAcceptsExplicitCurrentDirectory(t *testing.T) {
	got, err := validateDataRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("validated root = %q, want %q", got, want)
	}
}

func TestLoadFindsChannelDatabaseWhenDefaultDatabaseIsAbsent(t *testing.T) {
	for _, name := range []string{"opencode-local.db", "opencode-beta.db"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeNormalizerFixture(t, root)
			if err := os.Rename(filepath.Join(root, "opencode.db"), filepath.Join(root, name)); err != nil {
				t.Fatal(err)
			}

			result, err := Load(root, IngestOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Sessions) != 1 || len(result.Turns) != 2 {
				t.Fatalf("channel database result = %#v", result)
			}
		})
	}
}

func TestReaderReadsSyntheticHistoryReadOnlyAndInOrder(t *testing.T) {
	root := t.TempDir()
	dbPath := writeReaderFixture(t, root)
	reader, err := OpenReader(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()

	var got []Row
	if err := reader.Read(func(row Row) error {
		got = append(got, row)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantKinds := []RowKind{RowSession, RowSession, RowMessage, RowPart, RowMessage, RowPart, RowMessage, RowPart}
	if len(got) != len(wantKinds) {
		t.Fatalf("row count = %d, want %d (%#v)", len(got), len(wantKinds), got)
	}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Fatalf("row %d kind = %q, want %q", i, got[i].Kind, want)
		}
	}
	if got[0].Session.ID != "s1" || got[1].Session.ID != "s2" {
		t.Fatalf("sessions not sorted: %#v", got[:2])
	}
	if got[2].Message.ID != "m1" || got[3].Part.ID != "p1" || got[4].Message.ID != "m2" || got[5].Part.ID != "p2" {
		t.Fatalf("messages and parts not sorted: %#v", got[2:])
	}
	if got[3].Part.Data == nil || string(got[3].Part.Data) != `{"type":"tool"}` {
		t.Fatalf("part payload = %s", got[3].Part.Data)
	}

	var queryOnly int
	if err := reader.db.QueryRow("PRAGMA query_only").Scan(&queryOnly); err != nil {
		t.Fatal(err)
	}
	if queryOnly != 1 {
		t.Fatalf("query_only = %d, want 1", queryOnly)
	}
	if _, err := reader.db.Exec("CREATE TABLE should_not_exist (id TEXT)"); err == nil {
		t.Fatal("read-only reader accepted a write")
	}
}

func TestReaderRejectsMissingSchema(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE session (id TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReader(dbPath); err == nil || !errors.Is(err, ErrSchema) {
		t.Fatalf("schema error = %v, want ErrSchema", err)
	}
}

func TestLoadRootErrorsIncludePathAndMissingDatabaseIsEmpty(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := Load(missing, IngestOptions{}); err == nil || !containsErrorPath(err, missing) {
		t.Fatalf("missing root error = %v", err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file, IngestOptions{}); err == nil || !containsErrorPath(err, file) {
		t.Fatalf("file root error = %v", err)
	}
	empty := t.TempDir()
	result, err := Load(empty, IngestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 0 || len(result.Sessions) != 0 || len(result.Warnings) != 0 {
		t.Fatalf("empty root result = %#v", result)
	}
}

func writeReaderFixture(t *testing.T, root string) string {
	t.Helper()
	dbPath := filepath.Join(root, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	statements := []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY, directory TEXT, version TEXT, time_created INTEGER, time_updated INTEGER)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT, session_id TEXT, time_created INTEGER, time_updated INTEGER, data TEXT)`,
		`INSERT INTO session VALUES ('s2', '/two', '2', 2000, 3000)`,
		`INSERT INTO session VALUES ('s1', '/one', '1', 1000, 2000)`,
		`INSERT INTO message VALUES ('m2', 's1', 2000, 2000, '{"role":"assistant"}')`,
		`INSERT INTO message VALUES ('m1', 's1', 1000, 1000, '{"role":"user"}')`,
		`INSERT INTO message VALUES ('m3', 's2', 1000, 1000, '{"role":"user"}')`,
		`INSERT INTO part VALUES ('p2', 'm2', 's1', 1000, 1000, '{"type":"text"}')`,
		`INSERT INTO part VALUES ('p1', 'm1', 's1', 1000, 1000, '{"type":"tool"}')`,
		`INSERT INTO part VALUES ('p3', 'm3', 's2', 1000, 1000, '{"type":"text"}')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return dbPath
}

func containsErrorPath(err error, path string) bool {
	return err != nil && filepath.Clean(path) != "" && strings.Contains(err.Error(), path)
}
