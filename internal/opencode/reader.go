package opencode

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrSchema = errors.New("unsupported OpenCode database schema")

type RowKind string

const (
	RowSession RowKind = "session"
	RowMessage RowKind = "message"
	RowPart    RowKind = "part"
)

type Row struct {
	Kind    RowKind
	Session SessionRow
	Message MessageRow
	Part    PartRow
}

type SessionRow struct {
	ID        string
	Title     string
	Directory string
	Version   string
	Model     []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

type MessageRow struct {
	ID        string
	SessionID string
	CreatedAt time.Time
	UpdatedAt time.Time
	Data      []byte
}

type PartRow struct {
	ID        string
	MessageID string
	SessionID string
	CreatedAt time.Time
	UpdatedAt time.Time
	Data      []byte
}

type Reader struct {
	db   *sql.DB
	path string
}

func OpenReader(path string) (*Reader, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return nil, errors.New("OpenCode database path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open OpenCode database %q: %w", path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("OpenCode database %q is a directory", path)
	}
	if info.Mode().Perm()&0444 == 0 {
		return nil, fmt.Errorf("open OpenCode database %q: permission denied", path)
	}
	dsn := sqliteReadOnlyDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open OpenCode database %q: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	reader := &Reader{db: db, path: path}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open OpenCode database %q: %w", path, err)
	}
	var queryOnly int
	if err := db.QueryRow("PRAGMA query_only").Scan(&queryOnly); err != nil || queryOnly != 1 {
		_ = db.Close()
		if err == nil {
			err = errors.New("query_only is disabled")
		}
		return nil, fmt.Errorf("open OpenCode database %q as read-only: %w", path, err)
	}
	if err := reader.validateSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open OpenCode database %q: %w", path, err)
	}
	return reader, nil
}

func sqliteReadOnlyDSN(path string) string {
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	uri.RawQuery = "mode=ro&_query_only=1"
	return uri.String()
}

func (r *Reader) validateSchema() error {
	rows, err := r.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name IN ('session', 'message', 'part')`)
	if err != nil {
		return fmt.Errorf("%w: inspect tables: %v", ErrSchema, err)
	}
	defer func() { _ = rows.Close() }()
	found := make(map[string]struct{}, 3)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("%w: scan table list: %v", ErrSchema, err)
		}
		found[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w: read table list: %v", ErrSchema, err)
	}
	for _, name := range []string{"session", "message", "part"} {
		if _, ok := found[name]; !ok {
			return fmt.Errorf("%w: missing table %q", ErrSchema, name)
		}
	}
	return nil
}

func (r *Reader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// Read visits rows in deterministic order without materializing the history.
func (r *Reader) Read(consume func(Row) error) (err error) {
	if r == nil || r.db == nil {
		return errors.New("OpenCode reader is nil")
	}
	if consume == nil {
		consume = func(Row) error { return nil }
	}
	tx, err := r.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin OpenCode read transaction: %w", err)
	}
	if err := r.readSessions(tx, consume); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := r.readMessagesAndParts(tx, consume); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit OpenCode read transaction: %w", err)
	}
	return nil
}

type queryer interface {
	Query(string, ...any) (*sql.Rows, error)
}

func (r *Reader) readSessions(db queryer, consume func(Row) error) error {
	hasTitle, err := sessionHasColumn(db, "title")
	if err != nil {
		return fmt.Errorf("inspect OpenCode session columns: %w", err)
	}
	hasModel, err := sessionHasColumn(db, "model")
	if err != nil {
		return fmt.Errorf("inspect OpenCode session columns: %w", err)
	}
	columns := []string{"id", "directory"}
	if hasTitle {
		columns = append(columns, "title")
	}
	columns = append(columns, "version", "CAST(time_created AS TEXT)", "CAST(time_updated AS TEXT)")
	if hasModel {
		columns = append(columns, "model")
	}
	query := "SELECT " + strings.Join(columns, ", ") + " FROM session ORDER BY id"
	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("read OpenCode sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, title, directory, version, created, updated, model sql.NullString
		scanArgs := []any{&id, &directory}
		if hasTitle {
			scanArgs = append(scanArgs, &title)
		}
		scanArgs = append(scanArgs, &version, &created, &updated)
		if hasModel {
			scanArgs = append(scanArgs, &model)
		}
		scanErr := rows.Scan(scanArgs...)
		if scanErr != nil {
			return fmt.Errorf("read OpenCode session row: %w", scanErr)
		}
		row := Row{Kind: RowSession, Session: SessionRow{
			ID:        id.String,
			Title:     title.String,
			Directory: directory.String,
			Version:   version.String,
			Model:     append([]byte(nil), []byte(model.String)...),
			CreatedAt: parseDatabaseTime(created.String),
			UpdatedAt: parseDatabaseTime(updated.String),
		}}
		if err := consume(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

func sessionHasColumn(db queryer, wanted string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(session)`)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if strings.EqualFold(name, wanted) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (r *Reader) readMessagesAndParts(db queryer, consume func(Row) error) error {
	rows, err := db.Query(`
		SELECT m.id, m.session_id, CAST(m.time_created AS TEXT), CAST(m.time_updated AS TEXT), m.data,
		       p.id, p.message_id, p.session_id, CAST(p.time_created AS TEXT), CAST(p.time_updated AS TEXT), p.data
		FROM message AS m
		LEFT JOIN part AS p ON p.message_id = m.id
		ORDER BY m.session_id, m.time_created, m.id, p.time_created, p.id`)
	if err != nil {
		return fmt.Errorf("read OpenCode messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	lastMessageID := ""
	for rows.Next() {
		var messageID, messageSessionID, messageCreated, messageUpdated, messageData sql.NullString
		var partID, partMessageID, partSessionID, partCreated, partUpdated, partData sql.NullString
		if err := rows.Scan(&messageID, &messageSessionID, &messageCreated, &messageUpdated, &messageData, &partID, &partMessageID, &partSessionID, &partCreated, &partUpdated, &partData); err != nil {
			return fmt.Errorf("read OpenCode message row: %w", err)
		}
		if messageID.String == "" {
			continue
		}
		if messageID.String != lastMessageID {
			lastMessageID = messageID.String
			message := Row{Kind: RowMessage, Message: MessageRow{
				ID:        messageID.String,
				SessionID: messageSessionID.String,
				CreatedAt: parseDatabaseTime(messageCreated.String),
				UpdatedAt: parseDatabaseTime(messageUpdated.String),
				Data:      append([]byte(nil), []byte(messageData.String)...),
			}}
			if err := consume(message); err != nil {
				return err
			}
		}
		if !partID.Valid || partID.String == "" {
			continue
		}
		part := Row{Kind: RowPart, Part: PartRow{
			ID:        partID.String,
			MessageID: partMessageID.String,
			SessionID: partSessionID.String,
			CreatedAt: parseDatabaseTime(partCreated.String),
			UpdatedAt: parseDatabaseTime(partUpdated.String),
			Data:      append([]byte(nil), []byte(partData.String)...),
		}}
		if err := consume(part); err != nil {
			return err
		}
	}
	return rows.Err()
}

func Read(path string, consume func(Row) error) (err error) {
	reader, err := OpenReader(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := reader.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close OpenCode database %q: %w", path, closeErr)
		}
	}()
	return reader.Read(consume)
}

func parseDatabaseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if number, err := strconv.ParseInt(value, 10, 64); err == nil {
		if number > 1_000_000_000_000 {
			return time.UnixMilli(number).UTC()
		}
		return time.Unix(number, 0).UTC()
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
