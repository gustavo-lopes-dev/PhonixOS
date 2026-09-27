package database

import (
	"path/filepath"
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
