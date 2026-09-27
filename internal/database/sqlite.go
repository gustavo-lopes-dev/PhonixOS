package database

import (
	"database/sql"
	"fmt"
	"net/url"
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
	"foreign_keys=ON",
	"synchronous=NORMAL",
	"cache_size=-2000",
	"temp_store=MEMORY",
	"busy_timeout=5000",
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

	db, err := openConfiguredDB(dbPath, "")
	if err != nil {
		return nil, "", fmt.Errorf("open sqlite database %q: %w", dbPath, err)
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
		// TRUNCATE, ao contrário de WAL, não persiste no arquivo do banco.
		// Reabrir com o PRAGMA no DSN garante o modo em cada nova conexão.
		if err := db.Close(); err != nil {
			return nil, "", fmt.Errorf("close database before TRUNCATE configuration: %w", err)
		}
		db, err = openConfiguredDB(dbPath, journalModeTruncate)
		if err != nil {
			return nil, "", fmt.Errorf("open sqlite database in TRUNCATE mode %q: %w", dbPath, err)
		}
		applied, err := currentJournalMode(db)
		if err != nil || applied != journalModeTruncate {
			if err == nil {
				err = fmt.Errorf("unexpected journal_mode %q after TRUNCATE configuration", applied)
			}
			return nil, "", closeWithError(db, err)
		}
	}

	return db, mode, nil
}

// openConfiguredDB aplica os PRAGMAs por conexão, inclusive após uma conexão
// ser descartada e recriada pelo pool de database/sql.
func openConfiguredDB(dbPath, journalMode string) (*sql.DB, error) {
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolve database path %q: %w", dbPath, err)
	}
	query := url.Values{}
	for _, pragma := range basePragmas {
		query.Add("_pragma", pragma)
	}
	if journalMode != "" {
		query.Add("_pragma", "journal_mode="+journalMode)
	}
	dsn := (&url.URL{Scheme: "file", Path: absPath, RawQuery: query.Encode()}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func applyJournalMode(db *sql.DB, mode string) (string, error) {
	var applied string
	if err := db.QueryRow("PRAGMA journal_mode = " + mode + ";").Scan(&applied); err != nil {
		return "", fmt.Errorf("set journal_mode %s: %w", mode, err)
	}
	return strings.ToLower(applied), nil
}

func currentJournalMode(db *sql.DB) (string, error) {
	var applied string
	if err := db.QueryRow("PRAGMA journal_mode;").Scan(&applied); err != nil {
		return "", fmt.Errorf("read journal_mode: %w", err)
	}
	return strings.ToLower(applied), nil
}

func closeWithError(db *sql.DB, err error) error {
	if closeErr := db.Close(); closeErr != nil {
		return fmt.Errorf("%w (close database: %v)", err, closeErr)
	}
	return err
}
