package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	message_count   INTEGER,
	created         TEXT,
	modified        TEXT,
	git_branch      TEXT,
	is_sidechain    INTEGER DEFAULT 0,
	jsonl_path      TEXT,
	last_scanned    TEXT,
	is_worktree     INTEGER,
	repo_key        TEXT,
	parent_project_name TEXT,
	worktree_name   TEXT
);
`

// worktreeColumns are added to older databases via ALTER TABLE in migrate().
// is_worktree is deliberately nullable: NULL means "never resolved" (preserve),
// 0/1 mean a known non-worktree / worktree.
var worktreeColumns = []struct{ name, decl string }{
	{"is_worktree", "INTEGER"},
	{"repo_key", "TEXT"},
	{"parent_project_name", "TEXT"},
	{"worktree_name", "TEXT"},
}

// sessionColumns is listed explicitly so databases created by older versions,
// which carry dropped columns, still scan in a known order.
const sessionColumns = `session_id, project_name, project_path, claude_dir,
	first_prompt, existing_summary, title, message_count, created, modified,
	git_branch, is_sidechain, jsonl_path, last_scanned,
	is_worktree, repo_key, parent_project_name, worktree_name`

// Session is the DB representation, extending scanner.Session with the cached title.
type Session struct {
	SessionID       string
	ProjectName     string
	ProjectPath     string
	ClaudeDir       string
	FirstPrompt     string
	ExistingSummary string
	Title           string
	MessageCount    int
	Created         time.Time
	Modified        time.Time
	GitBranch       string
	IsSidechain     bool
	JSONLPath       string
	LastScanned     time.Time

	IsWorktree        bool
	RepoKey           string
	ParentProjectName string
	WorktreeName      string
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
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrating schema: %w", err)
	}
	return &DB{conn: conn}, nil
}

// migrate adds columns introduced after a database was first created. SQLite has
// no "ADD COLUMN IF NOT EXISTS", so we diff against table_info first. Idempotent.
func migrate(conn *sql.DB) error {
	rows, err := conn.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		return err
	}
	existing := make(map[string]bool)
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, c := range worktreeColumns {
		if existing[c.name] {
			continue
		}
		if _, err := conn.Exec("ALTER TABLE sessions ADD COLUMN " + c.name + " " + c.decl); err != nil {
			return err
		}
	}
	return nil
}

// worktreeArgs returns the four worktree upsert arguments. When resolution could
// not inspect the project on disk they are all NULL, so the COALESCE in Upsert
// preserves whatever was stored. When resolved they are concrete (including 0
// and ""), so a project that stopped being a worktree is cleared.
func worktreeArgs(s scanner.Session) (isWorktree, repoKey, parentName, worktreeName any) {
	if !s.WorktreeResolved {
		return nil, nil, nil, nil
	}
	return boolToInt(s.IsWorktree), s.RepoKey, s.ParentProjectName, s.WorktreeName
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

// Upsert inserts or updates sessions from a scan. Preserves the cached title.
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
			 first_prompt, existing_summary, title, message_count, created, modified,
			 git_branch, is_sidechain, jsonl_path, last_scanned,
			 is_worktree, repo_key, parent_project_name, worktree_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			project_name = excluded.project_name,
			project_path = excluded.project_path,
			claude_dir = excluded.claude_dir,
			first_prompt = COALESCE(excluded.first_prompt, sessions.first_prompt),
			existing_summary = COALESCE(excluded.existing_summary, sessions.existing_summary),
			-- Keep an already-harvested title when a later scan finds none,
			-- e.g. the session now resolves via the stale index instead.
			title = COALESCE(NULLIF(excluded.title, ''), sessions.title),
			message_count = COALESCE(excluded.message_count, sessions.message_count),
			created = COALESCE(excluded.created, sessions.created),
			modified = COALESCE(excluded.modified, sessions.modified),
			git_branch = excluded.git_branch,
			is_sidechain = excluded.is_sidechain,
			jsonl_path = excluded.jsonl_path,
			last_scanned = excluded.last_scanned,
			-- Overwrite when this scan resolved worktree state (non-NULL, even
			-- when 0/""); preserve stored values when it could not inspect.
			is_worktree = COALESCE(excluded.is_worktree, sessions.is_worktree),
			repo_key = COALESCE(excluded.repo_key, sessions.repo_key),
			parent_project_name = COALESCE(excluded.parent_project_name, sessions.parent_project_name),
			worktree_name = COALESCE(excluded.worktree_name, sessions.worktree_name)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range sessions {
		wtIsWorktree, wtRepoKey, wtParentName, wtName := worktreeArgs(s)
		_, err := stmt.Exec(
			s.SessionID, s.ProjectName, s.ProjectPath, s.ClaudeDir,
			s.FirstPrompt, s.ExistingSummary, s.Title, s.MessageCount,
			formatTime(s.Created), formatTime(s.Modified),
			s.GitBranch, boolToInt(s.IsSidechain), s.JSONLPath, now,
			wtIsWorktree, wtRepoKey, wtParentName, wtName,
		)
		if err != nil {
			return fmt.Errorf("upserting session %s: %w", s.SessionID, err)
		}
	}

	return tx.Commit()
}

// allowedOrderClauses maps valid sortBy values to pre-built ORDER BY clauses.
// Using a whitelist of static strings prevents SQL injection via sortBy.
var allowedOrderClauses = map[string]map[bool]string{
	"modified": {
		true:  "modified DESC",
		false: "modified ASC",
	},
	"created": {
		true:  "created DESC",
		false: "created ASC",
	},
	"messages": {
		true:  "message_count DESC",
		false: "message_count ASC",
	},
	"project": {
		true:  "project_name DESC, modified DESC",
		false: "project_name ASC, modified DESC",
	},
}

// AllSessions returns sessions sorted by the given field.
func (db *DB) AllSessions(sortBy string, desc bool) ([]Session, error) {
	clauses, ok := allowedOrderClauses[sortBy]
	if !ok {
		clauses = allowedOrderClauses["modified"]
	}
	orderClause := clauses[desc]

	query := "SELECT " + sessionColumns + " FROM sessions ORDER BY " + orderClause
	return db.querySessions(query)
}

// Search returns sessions matching a text query across multiple fields.
func (db *DB) Search(query string) ([]Session, error) {
	like := "%" + query + "%"
	return db.querySessions(
		`SELECT `+sessionColumns+` FROM sessions WHERE
			project_name LIKE ?1 OR title LIKE ?1
			OR first_prompt LIKE ?1 OR git_branch LIKE ?1 OR existing_summary LIKE ?1
			OR parent_project_name LIKE ?1 OR worktree_name LIKE ?1
		ORDER BY modified DESC`,
		like,
	)
}

// PruneUnseen deletes sessions that were not part of the current scan.
// This handles deleted sessions, removed scan paths, etc.
//
// Sessions that carry a harvested title are kept even once they stop being
// scannable, so a title survives Claude Code deleting the transcript. They are
// still dropped when their project leaves retainPaths, otherwise removing a scan
// path would leave its sessions behind forever. Pass no retainPaths to prune
// every unseen session.
func (db *DB) PruneUnseen(currentIDs []string, retainPaths []string) (int64, error) {
	if len(currentIDs) == 0 {
		return 0, nil
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Create a temp table with current IDs
	if _, err := tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS seen_ids (session_id TEXT PRIMARY KEY)`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM seen_ids`); err != nil {
		return 0, err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO seen_ids (session_id) VALUES (?)`)
	if err != nil {
		return 0, err
	}
	for _, id := range currentIDs {
		if _, err := stmt.Exec(id); err != nil {
			stmt.Close()
			return 0, err
		}
	}
	stmt.Close()

	if err := fillRetainPaths(tx, retainPaths); err != nil {
		return 0, err
	}

	result, err := tx.Exec(`
		DELETE FROM sessions
		WHERE session_id NOT IN (SELECT session_id FROM seen_ids)
		  AND NOT (
			COALESCE(title, '') <> ''
			AND EXISTS (
				SELECT 1 FROM retain_paths rp
				WHERE sessions.project_path = rp.path
				   OR sessions.project_path LIKE rp.path || '/%'
			)
		  )`)
	if err != nil {
		return 0, err
	}
	pruned, _ := result.RowsAffected()

	if _, err := tx.Exec(`DROP TABLE IF EXISTS seen_ids`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DROP TABLE IF EXISTS retain_paths`); err != nil {
		return 0, err
	}
	return pruned, tx.Commit()
}

func fillRetainPaths(tx *sql.Tx, paths []string) error {
	if _, err := tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS retain_paths (path TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM retain_paths`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO retain_paths (path) VALUES (?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := stmt.Exec(strings.TrimSuffix(p, "/")); err != nil {
			return err
		}
	}
	return nil
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
			projectPath       sql.NullString
			firstPrompt       sql.NullString
			existingSummary   sql.NullString
			title             sql.NullString
			created           sql.NullString
			modified          sql.NullString
			gitBranch         sql.NullString
			jsonlPath         sql.NullString
			lastScanned       sql.NullString
			isSidechain       int
			messageCount      sql.NullInt64
			isWorktree        sql.NullInt64
			repoKey           sql.NullString
			parentProjectName sql.NullString
			worktreeName      sql.NullString
		)
		err := rows.Scan(
			&s.SessionID, &s.ProjectName, &projectPath, &s.ClaudeDir,
			&firstPrompt, &existingSummary, &title,
			&messageCount, &created, &modified,
			&gitBranch, &isSidechain, &jsonlPath, &lastScanned,
			&isWorktree, &repoKey, &parentProjectName, &worktreeName,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		s.ProjectPath = projectPath.String
		s.FirstPrompt = firstPrompt.String
		s.ExistingSummary = existingSummary.String
		s.Title = title.String
		s.MessageCount = int(messageCount.Int64)
		s.Created = parseTime(created.String)
		s.Modified = parseTime(modified.String)
		s.GitBranch = gitBranch.String
		s.IsSidechain = isSidechain != 0
		s.JSONLPath = jsonlPath.String
		s.LastScanned = parseTime(lastScanned.String)
		s.IsWorktree = isWorktree.Valid && isWorktree.Int64 == 1
		s.RepoKey = repoKey.String
		s.ParentProjectName = parentProjectName.String
		s.WorktreeName = worktreeName.String
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
