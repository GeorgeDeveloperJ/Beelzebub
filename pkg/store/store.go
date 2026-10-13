package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// EventStatus defines the outcome of a sentinel check or intervention.
type EventStatus string

const (
	StatusPass        EventStatus = "PASS"
	StatusRemediating EventStatus = "REMEDIATING"
	StatusHealed      EventStatus = "HEALED"
	StatusExhausted   EventStatus = "EXHAUSTED"
	StatusPending     EventStatus = "PENDING"
)

// Event represents an audit log entry in SQLite.
type Event struct {
	ID           int64       `json:"id"`
	SentinelName string      `json:"sentinel_name"`
	Timestamp    time.Time   `json:"timestamp"`
	Status       EventStatus `json:"status"`
	Stdout       string      `json:"stdout"`
	Stderr       string      `json:"stderr"`
	DurationMs   int64       `json:"duration_ms"`
}

// SentinelState represents current runtime health state of a sentinel.
type SentinelState struct {
	SentinelName        string      `json:"sentinel_name"`
	Status              EventStatus `json:"status"`
	ConsecutiveFailures int         `json:"consecutive_failures"`
	LastCheckedAt       time.Time   `json:"last_checked_at"`
	LastDurationMs      int64       `json:"last_duration_ms"`
	LastOutput          string      `json:"last_output"`
	UpdatedAt           time.Time   `json:"updated_at"`
}

// Store wraps the SQLite database connection with thread safety.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewStore opens or creates the SQLite database, configures WAL mode, and executes migrations.
func NewStore(dbPath string) (*Store, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory %q: %w", dir, err)
		}
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database %q: %w", dbPath, err)
	}

	db.SetMaxOpenConns(1) // SQLite works best with serialized writes

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return s, nil
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sentinel_name TEXT NOT NULL,
		timestamp DATETIME NOT NULL,
		status TEXT NOT NULL,
		stdout TEXT,
		stderr TEXT,
		duration_ms INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_events_name_time ON events(sentinel_name, timestamp DESC);

	CREATE TABLE IF NOT EXISTS sentinel_state (
		sentinel_name TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		consecutive_failures INTEGER NOT NULL DEFAULT 0,
		last_checked_at DATETIME NOT NULL,
		last_duration_ms INTEGER NOT NULL DEFAULT 0,
		last_output TEXT,
		updated_at DATETIME NOT NULL
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// RecordEvent writes an audit log event into the events table.
func (s *Store) RecordEvent(e *Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	query := `
	INSERT INTO events (sentinel_name, timestamp, status, stdout, stderr, duration_ms)
	VALUES (?, ?, ?, ?, ?, ?);
	`
	res, err := s.db.Exec(query, e.SentinelName, e.Timestamp, string(e.Status), e.Stdout, e.Stderr, e.DurationMs)
	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	id, err := res.LastInsertId()
	if err == nil {
		e.ID = id
	}
	return nil
}

// GetRecentEvents retrieves the newest audit log events up to limit.
func (s *Store) GetRecentEvents(limit int) ([]Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	SELECT id, sentinel_name, timestamp, status, stdout, stderr, duration_ms
	FROM events
	ORDER BY id DESC
	LIMIT ?;
	`
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var statusStr string
		if err := rows.Scan(&e.ID, &e.SentinelName, &e.Timestamp, &statusStr, &e.Stdout, &e.Stderr, &e.DurationMs); err != nil {
			return nil, fmt.Errorf("failed to scan event row: %w", err)
		}
		e.Status = EventStatus(statusStr)
		events = append(events, e)
	}
	return events, rows.Err()
}

// UpdateSentinelState updates or creates a sentinel's current health state.
func (s *Store) UpdateSentinelState(state *SentinelState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	state.UpdatedAt = now
	if state.LastCheckedAt.IsZero() {
		state.LastCheckedAt = now
	}

	query := `
	INSERT INTO sentinel_state (sentinel_name, status, consecutive_failures, last_checked_at, last_duration_ms, last_output, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(sentinel_name) DO UPDATE SET
		status = excluded.status,
		consecutive_failures = excluded.consecutive_failures,
		last_checked_at = excluded.last_checked_at,
		last_duration_ms = excluded.last_duration_ms,
		last_output = excluded.last_output,
		updated_at = excluded.updated_at;
	`
	_, err := s.db.Exec(query,
		state.SentinelName,
		string(state.Status),
		state.ConsecutiveFailures,
		state.LastCheckedAt,
		state.LastDurationMs,
		state.LastOutput,
		state.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert sentinel state: %w", err)
	}
	return nil
}

// GetSentinelState retrieves state for a given sentinel.
func (s *Store) GetSentinelState(name string) (*SentinelState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	SELECT sentinel_name, status, consecutive_failures, last_checked_at, last_duration_ms, last_output, updated_at
	FROM sentinel_state
	WHERE sentinel_name = ?;
	`
	var state SentinelState
	var statusStr string
	err := s.db.QueryRow(query, name).Scan(
		&state.SentinelName,
		&statusStr,
		&state.ConsecutiveFailures,
		&state.LastCheckedAt,
		&state.LastDurationMs,
		&state.LastOutput,
		&state.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get sentinel state: %w", err)
	}
	state.Status = EventStatus(statusStr)
	return &state, nil
}

// GetAllSentinelStates retrieves all known sentinel health states.
func (s *Store) GetAllSentinelStates() ([]SentinelState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
	SELECT sentinel_name, status, consecutive_failures, last_checked_at, last_duration_ms, last_output, updated_at
	FROM sentinel_state
	ORDER BY sentinel_name ASC;
	`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all sentinel states: %w", err)
	}
	defer rows.Close()

	var list []SentinelState
	for rows.Next() {
		var state SentinelState
		var statusStr string
		if err := rows.Scan(
			&state.SentinelName,
			&statusStr,
			&state.ConsecutiveFailures,
			&state.LastCheckedAt,
			&state.LastDurationMs,
			&state.LastOutput,
			&state.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan sentinel state: %w", err)
		}
		state.Status = EventStatus(statusStr)
		list = append(list, state)
	}
	return list, rows.Err()
}

// ResetSentinelState clears failures and sets status back to PASS.
func (s *Store) ResetSentinelState(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `
	UPDATE sentinel_state
	SET status = ?, consecutive_failures = 0, updated_at = ?
	WHERE sentinel_name = ?;
	`
	_, err := s.db.Exec(query, string(StatusPass), time.Now().UTC(), name)
	return err
}

// SyncSentinelStates ensures sentinel_state contains exactly the configured active sentinels.
// It deletes stale sentinels no longer present in activeNames, and seeds any new ones.
func (s *Store) SyncSentinelStates(activeNames []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	activeSet := make(map[string]bool, len(activeNames))
	for _, name := range activeNames {
		activeSet[name] = true
	}

	// 1. Read existing names
	rows, err := s.db.Query("SELECT sentinel_name FROM sentinel_state;")
	if err != nil {
		return fmt.Errorf("failed to query sentinel names for sync: %w", err)
	}
	defer rows.Close()

	var toDelete []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			if !activeSet[name] {
				toDelete = append(toDelete, name)
			}
		}
	}
	_ = rows.Close()

	// 2. Prune stale sentinels
	for _, delName := range toDelete {
		_, _ = s.db.Exec("DELETE FROM sentinel_state WHERE sentinel_name = ?;", delName)
	}

	// 3. Seed newly added sentinels
	now := time.Now().UTC()
	for _, name := range activeNames {
		var count int
		_ = s.db.QueryRow("SELECT COUNT(*) FROM sentinel_state WHERE sentinel_name = ?;", name).Scan(&count)
		if count == 0 {
			query := `
			INSERT INTO sentinel_state (sentinel_name, status, consecutive_failures, last_checked_at, last_duration_ms, last_output, updated_at)
			VALUES (?, ?, 0, ?, 0, ?, ?);
			`
			_, _ = s.db.Exec(query, name, string(StatusPending), now, "Waiting for initial check", now)
		}
	}

	return nil
}
