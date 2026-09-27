package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	journalModeWAL      = "wal"
	journalModeTruncate = "truncate"
)

var basePragmas = []string{
	"PRAGMA foreign_keys = ON;",
	"PRAGMA synchronous = NORMAL;",
	"PRAGMA cache_size = -2000;",
	"PRAGMA temp_store = MEMORY;",
	"PRAGMA busy_timeout = 5000;",
}

func InitDB(dbPath string) (*sql.DB, string, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, "", fmt.Errorf("database path must not be empty")
	}

	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create database directory %q: %w", dir, err)
	}

	lockSupported, err := CheckPOSIXLockSupport(dir)
	if err != nil {
		return nil, "", fmt.Errorf("probe posix lock support in %q: %w", dir, err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, "", fmt.Errorf("open sqlite database %q: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, pragma := range basePragmas {
		if _, err := db.Exec(pragma); err != nil {
			return nil, "", closeWithError(db, fmt.Errorf("apply %q: %w", pragma, err))
		}
	}

	mode := journalModeTruncate
	if lockSupported {
		applied, walErr := applyJournalMode(db, "WAL")
		if walErr == nil && applied == journalModeWAL {
			mode = journalModeWAL
		}
	}

	if mode != journalModeWAL {
		applied, truncateErr := applyJournalMode(db, "TRUNCATE")
		if truncateErr != nil {
			return nil, "", closeWithError(db, truncateErr)
		}
		if applied != journalModeTruncate {
			return nil, "", closeWithError(db, fmt.Errorf("unexpected journal_mode %q after TRUNCATE request", applied))
		}
		mode = journalModeTruncate
	}

	return db, mode, nil
}

func applyJournalMode(db *sql.DB, mode string) (string, error) {
	var applied string
	if err := db.QueryRow("PRAGMA journal_mode = " + mode + ";").Scan(&applied); err != nil {
		return "", fmt.Errorf("set journal_mode %s: %w", mode, err)
	}
	return strings.ToLower(applied), nil
}

func closeWithError(db *sql.DB, err error) error {
	if closeErr := db.Close(); closeErr != nil {
		return fmt.Errorf("%w (close database: %v)", err, closeErr)
	}
	return err
}
