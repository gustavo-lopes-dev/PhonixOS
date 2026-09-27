package database

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func TestResolveDBPathPriority(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("APP_DATA_DIR", filepath.Join(dir, "app"))
	path, err := ResolveDBPath()
	if err != nil || path != filepath.Join(dir, "app", dbFileName) {
		t.Fatalf("APP_DATA_DIR path = %q, err = %v", path, err)
	}
	t.Setenv("APP_DATA_DIR", "")
	path, err = ResolveDBPath()
	if err != nil || path != filepath.Join(dir, "home", "data", dbFileName) {
		t.Fatalf("HOME path = %q, err = %v", path, err)
	}
}

func TestInitDBAndMigrations(t *testing.T) {
	db, mode, err := InitDB(filepath.Join(t.TempDir(), "phonix.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()

	var journal string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != mode || (mode != journalModeWAL && mode != journalModeTruncate) {
		t.Fatalf("journal mode = %q, selected = %q, err = %v", journal, mode, err)
	}
	for _, check := range []struct {
		pragma string
		want   int
	}{
		{"foreign_keys", 1},
		{"synchronous", 1},
		{"cache_size", -2000},
		{"temp_store", 2},
		{"busy_timeout", 5000},
	} {
		var got int
		if err := db.QueryRow("PRAGMA " + check.pragma).Scan(&got); err != nil || got != check.want {
			t.Errorf("%s = %d, want %d, err = %v", check.pragma, got, check.want, err)
		}
	}

	for i := 0; i < 2; i++ {
		if err := RunMigrations(db); err != nil {
			t.Fatalf("RunMigrations pass %d: %v", i+1, err)
		}
	}
	var versions int
	if err := db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version = '001_initial_schema.sql'").Scan(&versions); err != nil || versions != 1 {
		t.Fatalf("recorded initial migration = %d, err = %v", versions, err)
	}
	var cards int
	if err := db.QueryRow("SELECT count(*) FROM layout_cards").Scan(&cards); err != nil || cards != 4 {
		t.Fatalf("layout seed count = %d, want 4, err = %v", cards, err)
	}
	var instance string
	if err := db.QueryRow("SELECT instance_name FROM dashboard_settings WHERE id = 1").Scan(&instance); err != nil || instance != "Phonix Server" {
		t.Fatalf("settings seed = %q, err = %v", instance, err)
	}
	if _, err := db.Exec("INSERT INTO shortcuts (title, url, is_pinned) VALUES ('bad', 'https://example.org', 2)"); err == nil {
		t.Fatal("expected database to enforce is_pinned constraint")
	}
}

func TestPragmasOnReplacementConnection(t *testing.T) {
	db, mode, err := InitDB(filepath.Join(t.TempDir(), "phonix.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()

	// Com nenhum idle, cada consulta abre uma conexão física nova.
	db.SetMaxIdleConns(0)
	for i := 0; i < 2; i++ {
		for _, check := range []struct {
			name string
			want int
		}{
			{"foreign_keys", 1},
			{"synchronous", 1},
			{"cache_size", -2000},
			{"temp_store", 2},
			{"busy_timeout", 5000},
		} {
			var got int
			if err := db.QueryRow("PRAGMA " + check.name).Scan(&got); err != nil || got != check.want {
				t.Fatalf("connection %d: %s = %d, want %d: %v", i, check.name, got, check.want, err)
			}
		}
		var gotMode string
		if err := db.QueryRow("PRAGMA journal_mode").Scan(&gotMode); err != nil || gotMode != mode {
			t.Fatalf("connection %d: journal_mode = %q, want %q: %v", i, gotMode, mode, err)
		}
	}
}

func TestTruncateModeOnReplacementConnection(t *testing.T) {
	db, err := openConfiguredDB(filepath.Join(t.TempDir(), "phonix.db"), journalModeTruncate)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	db.SetMaxIdleConns(0)
	for i := 0; i < 2; i++ {
		mode, err := currentJournalMode(db)
		if err != nil || mode != journalModeTruncate {
			t.Fatalf("connection %d: journal_mode = %q, want truncate: %v", i, mode, err)
		}
		var foreignKeys int
		if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
			t.Fatalf("connection %d: foreign_keys = %d, want 1: %v", i, foreignKeys, err)
		}
	}
}

func TestMigrationRollbackAndRetry(t *testing.T) {
	db, _, err := InitDB(filepath.Join(t.TempDir(), "phonix.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}

	name := "002_rollback.sql"
	if err := applyMigration(db, name, "CREATE TABLE rollback_test (id INTEGER); INSERT INTO missing_table VALUES (1);"); err == nil {
		t.Fatal("expected migration to fail")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'rollback_test'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partially applied schema: %d, %v", count, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM schema_migrations WHERE version = ?", name).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration recorded: %d, %v", count, err)
	}
	if err := applyMigration(db, name, "CREATE TABLE rollback_test (id INTEGER);"); err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(db, name, "CREATE TABLE must_not_exist (id INTEGER);"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'must_not_exist'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("already applied migration ran twice: %d, %v", count, err)
	}
}

func TestExistingSchemaAdoptsMigrationHistory(t *testing.T) {
	db, _, err := InitDB(filepath.Join(t.TempDir(), "phonix.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	initial, err := migrationsFS.ReadFile("migrations/001_initial_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(initial)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE dashboard_settings SET instance_name = 'Existing' WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.QueryRow("SELECT instance_name FROM dashboard_settings WHERE id = 1").Scan(&name); err != nil || name != "Existing" {
		t.Fatalf("existing settings lost: %q, %v", name, err)
	}
	var versions int
	if err := db.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&versions); err != nil || versions != 1 {
		t.Fatalf("migration history = %d, %v", versions, err)
	}
}

func TestConcurrentMigrationsOnlyApplyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phonix.db")
	first, _, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := first.Close(); err != nil {
			t.Error(err)
		}
	}()
	second, _, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := second.Close(); err != nil {
			t.Error(err)
		}
	}()

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, db := range []*sql.DB{first, second} {
		wg.Add(1)
		go func(db *sql.DB) {
			defer wg.Done()
			errs <- RunMigrations(db)
		}(db)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migration: %v", err)
		}
	}
	var versions int
	if err := first.QueryRow("SELECT count(*) FROM schema_migrations WHERE version = '001_initial_schema.sql'").Scan(&versions); err != nil || versions != 1 {
		t.Fatalf("migration applied %d times: %v", versions, err)
	}
}

func TestLockProbeUsesUniqueFileAndClassifiesErrors(t *testing.T) {
	dir := t.TempDir()
	supported, err := CheckPOSIXLockSupport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !supported {
		t.Skip("filesystem does not support POSIX locks")
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("lock probe files left behind: %v", files)
	}
	if !unsupportedLockError(syscall.ENOTSUP) || !unsupportedLockError(syscall.ENOSYS) || unsupportedLockError(syscall.EAGAIN) || unsupportedLockError(syscall.EACCES) || unsupportedLockError(errors.New("unknown")) {
		t.Fatal("unexpected POSIX lock error classification")
	}
}
