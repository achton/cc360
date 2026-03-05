package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/achton/cc360/internal/scanner"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	session_id      TEXT PRIMARY KEY,
	project_name    TEXT NOT NULL,
	project_path    TEXT,
	claude_dir      TEXT NOT NULL,
	first_prompt    TEXT,
	existing_summary TEXT,
	title           TEXT,
	summary         TEXT,
	message_count   INTEGER,
	created         TEXT,
	modified        TEXT,
	git_branch      TEXT,
	is_sidechain    INTEGER DEFAULT 0,
	jsonl_path      TEXT,
	last_scanned    TEXT,
	summarized_at   TEXT
);
`

// Session is the DB representation, extending scanner.Session with AI-generated fields.
type Session struct {
	SessionID       string
	ProjectName     string
	ProjectPath     string
	ClaudeDir       string
	FirstPrompt     string
	ExistingSummary string
	Title           string
	Summary         string
	MessageCount    int
	Created         time.Time
	Modified        time.Time
	GitBranch       string
	IsSidechain     bool
	JSONLPath       string
	LastScanned     time.Time
	SummarizedAt    time.Time
}

type DB struct {
	conn *sql.DB
}

func defaultPath() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cc360", "cc360.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "cc360", "cc360.db")
}

// Open creates or opens the SQLite database.
func Open(path string) (*DB, error) {
	if path == "" {
		path = defaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("creating cache dir: %w", err)
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	return &DB{conn: conn}, nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Upsert inserts or updates sessions from a scan. Preserves title/summary.
func (db *DB) Upsert(sessions []scanner.Session) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := formatTime(time.Now().UTC())
	stmt, err := tx.Prepare(`
		INSERT INTO sessions
			(session_id, project_name, project_path, claude_dir,
			 first_prompt, existing_summary, message_count, created, modified,
			 git_branch, is_sidechain, jsonl_path, last_scanned)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			project_name = excluded.project_name,
			project_path = excluded.project_path,
			claude_dir = excluded.claude_dir,
			first_prompt = COALESCE(excluded.first_prompt, sessions.first_prompt),
			existing_summary = COALESCE(excluded.existing_summary, sessions.existing_summary),
			message_count = COALESCE(excluded.message_count, sessions.message_count),
			created = COALESCE(excluded.created, sessions.created),
			modified = COALESCE(excluded.modified, sessions.modified),
			git_branch = COALESCE(excluded.git_branch, sessions.git_branch),
			is_sidechain = excluded.is_sidechain,
			jsonl_path = COALESCE(excluded.jsonl_path, sessions.jsonl_path),
			last_scanned = excluded.last_scanned
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range sessions {
		_, err := stmt.Exec(
			s.SessionID, s.ProjectName, s.ProjectPath, s.ClaudeDir,
			s.FirstPrompt, s.ExistingSummary, s.MessageCount,
			formatTime(s.Created), formatTime(s.Modified),
			s.GitBranch, boolToInt(s.IsSidechain), s.JSONLPath, now,
		)
		if err != nil {
			return fmt.Errorf("upserting session %s: %w", s.SessionID, err)
		}
	}

	return tx.Commit()
}

// AllSessions returns sessions sorted by the given field.
func (db *DB) AllSessions(sortBy string, desc bool) ([]Session, error) {
	orderCol := "modified"
	switch sortBy {
	case "created":
		orderCol = "created"
	case "messages":
		orderCol = "message_count"
	case "project":
		orderCol = "project_name"
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	// For project sort, secondary sort by modified DESC
	orderClause := fmt.Sprintf("%s %s", orderCol, dir)
	if sortBy == "project" {
		orderClause += ", modified DESC"
	}

	query := fmt.Sprintf("SELECT * FROM sessions ORDER BY %s", orderClause)
	return db.querySessions(query)
}

// Search returns sessions matching a text query across multiple fields.
func (db *DB) Search(query string) ([]Session, error) {
	like := "%" + query + "%"
	return db.querySessions(
		`SELECT * FROM sessions WHERE
			project_name LIKE ?1 OR title LIKE ?1 OR summary LIKE ?1
			OR first_prompt LIKE ?1 OR git_branch LIKE ?1 OR existing_summary LIKE ?1
		ORDER BY modified DESC`,
		like,
	)
}

// Unsummarized returns sessions without AI-generated titles.
func (db *DB) Unsummarized(limit int) ([]Session, error) {
	query := `SELECT * FROM sessions
		WHERE title IS NULL AND jsonl_path IS NOT NULL AND jsonl_path != ''
		ORDER BY modified DESC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	return db.querySessions(query)
}

// SetSummary stores an AI-generated title and summary.
func (db *DB) SetSummary(sessionID, title, summary string) error {
	now := formatTime(time.Now().UTC())
	_, err := db.conn.Exec(
		`UPDATE sessions SET title = ?, summary = ?, summarized_at = ? WHERE session_id = ?`,
		title, summary, now, sessionID,
	)
	return err
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) querySessions(query string, args ...any) ([]Session, error) {
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		var (
			projectPath     sql.NullString
			firstPrompt     sql.NullString
			existingSummary sql.NullString
			title           sql.NullString
			summary         sql.NullString
			created         sql.NullString
			modified        sql.NullString
			gitBranch       sql.NullString
			jsonlPath       sql.NullString
			lastScanned     sql.NullString
			summarizedAt    sql.NullString
			isSidechain     int
			messageCount    sql.NullInt64
		)
		err := rows.Scan(
			&s.SessionID, &s.ProjectName, &projectPath, &s.ClaudeDir,
			&firstPrompt, &existingSummary, &title, &summary,
			&messageCount, &created, &modified,
			&gitBranch, &isSidechain, &jsonlPath,
			&lastScanned, &summarizedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		s.ProjectPath = projectPath.String
		s.FirstPrompt = firstPrompt.String
		s.ExistingSummary = existingSummary.String
		s.Title = title.String
		s.Summary = summary.String
		s.MessageCount = int(messageCount.Int64)
		s.Created = parseTime(created.String)
		s.Modified = parseTime(modified.String)
		s.GitBranch = gitBranch.String
		s.IsSidechain = isSidechain != 0
		s.JSONLPath = jsonlPath.String
		s.LastScanned = parseTime(lastScanned.String)
		s.SummarizedAt = parseTime(summarizedAt.String)
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
