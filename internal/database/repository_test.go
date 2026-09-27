package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, _, err := InitDB(filepath.Join(t.TempDir(), "phonix.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}
	return NewStore(db)
}

func stringPtr(value string) *string { return &value }
func intPtr(value int) *int          { return &value }
func boolPtr(value bool) *bool       { return &value }

func TestShortcutLifecycle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	created, err := store.CreateShortcut(ctx, CreateShortcutDTO{Title: "Pi-hole", URL: "http://192.168.1.2/admin"})
	if err != nil {
		t.Fatalf("CreateShortcut defaults: %v", err)
	}
	if created.ID == 0 || created.IconURL != nil || created.Category != defaultShortcutCategory || created.IsPinned || created.DisplayOrder != 0 {
		t.Fatalf("unexpected defaults: %+v", created)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not populated: %+v", created)
	}

	icon := "https://example.org/favicon.ico"
	second, err := store.CreateShortcut(ctx, CreateShortcutDTO{
		Title: "Grafana", URL: "http://192.168.1.3:3000",
		IconURL: &icon, Category: stringPtr("Monitoramento"), DisplayOrder: intPtr(-1), IsPinned: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("CreateShortcut explicit: %v", err)
	}

	list, err := store.ListShortcuts(ctx)
	if err != nil {
		t.Fatalf("ListShortcuts: %v", err)
	}
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != created.ID {
		t.Fatalf("order by display_order ASC, id ASC broken: %+v", list)
	}
	if list[0].IconURL == nil || *list[0].IconURL != icon || !list[0].IsPinned {
		t.Fatalf("explicit fields not persisted: %+v", list[0])
	}

	updated, err := store.UpdateShortcut(ctx, created.ID, UpdateShortcutDTO{Title: stringPtr("Pi-hole DNS")})
	if err != nil {
		t.Fatalf("UpdateShortcut: %v", err)
	}
	if updated.Title != "Pi-hole DNS" || updated.URL != created.URL || updated.Category != defaultShortcutCategory {
		t.Fatalf("partial update merged wrong fields: %+v", updated)
	}

	if _, err := store.GetShortcut(ctx, 9999); !errors.Is(err, ErrShortcutNotFound) {
		t.Fatalf("GetShortcut missing: %v", err)
	}
	if _, err := store.UpdateShortcut(ctx, 9999, UpdateShortcutDTO{Title: stringPtr("x")}); !errors.Is(err, ErrShortcutNotFound) {
		t.Fatalf("UpdateShortcut missing: %v", err)
	}
	if err := store.DeleteShortcut(ctx, 9999); !errors.Is(err, ErrShortcutNotFound) {
		t.Fatalf("DeleteShortcut missing: %v", err)
	}
	if err := store.DeleteShortcut(ctx, created.ID); err != nil {
		t.Fatalf("DeleteShortcut: %v", err)
	}
	list, err = store.ListShortcuts(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("after delete: %v, %+v", err, list)
	}
}

func TestPartialUpdatesPreserveConcurrentFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	shortcut, err := store.CreateShortcut(ctx, CreateShortcutDTO{Title: "Original", URL: "http://local"})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 20; i++ {
		var wg sync.WaitGroup
		failures := make(chan error, 4)
		wg.Add(4)
		go func() {
			defer wg.Done()
			_, err := store.UpdateShortcut(ctx, shortcut.ID, UpdateShortcutDTO{Title: stringPtr(fmt.Sprintf("Title %d", i))})
			failures <- err
		}()
		go func() {
			defer wg.Done()
			_, err := store.UpdateShortcut(ctx, shortcut.ID, UpdateShortcutDTO{DisplayOrder: intPtr(i + 1)})
			failures <- err
		}()
		go func() {
			defer wg.Done()
			_, err := store.UpdateSettings(ctx, UpdateSettingsDTO{InstanceName: stringPtr(fmt.Sprintf("Server %d", i))})
			failures <- err
		}()
		go func() {
			defer wg.Done()
			_, err := store.UpdateSettings(ctx, UpdateSettingsDTO{CustomPollIntervalMS: intPtr(i + 1)})
			failures <- err
		}()
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		gotShortcut, err := store.GetShortcut(ctx, shortcut.ID)
		if err != nil || gotShortcut.Title != fmt.Sprintf("Title %d", i) || gotShortcut.DisplayOrder != i+1 {
			t.Fatalf("shortcut lost an update: %+v (%v)", gotShortcut, err)
		}
		gotSettings, err := store.GetSettings(ctx)
		if err != nil || gotSettings.InstanceName != fmt.Sprintf("Server %d", i) || gotSettings.CustomPollIntervalMS != i+1 {
			t.Fatalf("settings lost an update: %+v (%v)", gotSettings, err)
		}
	}
}

func TestUpdateLayoutCardsAtomic(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	seeded, err := store.ListLayoutCards(ctx)
	if err != nil || len(seeded) != 4 {
		t.Fatalf("layout seed: %v, %+v", err, seeded)
	}

	batch := []UpdateLayoutCardDTO{
		{CardKey: "metrics_cpu", PosX: 2, PosY: 1, Width: 2, Height: 2, IsVisible: false},
		{CardKey: "app_launcher", PosX: 0, PosY: 0, Width: 4, Height: 4, IsVisible: true},
	}
	updated, err := store.UpdateLayoutCards(ctx, batch)
	if err != nil {
		t.Fatalf("UpdateLayoutCards: %v", err)
	}
	if len(updated) != 4 {
		t.Fatalf("updated listing size: %d", len(updated))
	}
	cpu, err := store.GetLayoutCard(ctx, "metrics_cpu")
	if err != nil || cpu.PosX != 2 || cpu.PosY != 1 || cpu.IsVisible {
		t.Fatalf("card not updated: %v, %+v", err, cpu)
	}

	// Lote com chave desconhecida deve reverter integralmente.
	_, err = store.UpdateLayoutCards(ctx, []UpdateLayoutCardDTO{
		{CardKey: "metrics_memory", PosX: 5, PosY: 5, Width: 1, Height: 1, IsVisible: false},
		{CardKey: "unknown_card", PosX: 0, PosY: 0, Width: 1, Height: 1, IsVisible: true},
	})
	if !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound: %v", err)
	}
	mem, err := store.GetLayoutCard(ctx, "metrics_memory")
	if err != nil {
		t.Fatal(err)
	}
	if mem.PosX == 5 || mem.PosY == 5 || !mem.IsVisible {
		t.Fatalf("batch not rolled back: %+v", mem)
	}
	if _, err := store.GetLayoutCard(ctx, "unknown_card"); !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("GetLayoutCard missing: %v", err)
	}
}

func TestSettingsSingleton(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	settings, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings.ID != settingsSingletonID || settings.InstanceName != "Phonix Server" || settings.ThemeMode != "dark" || settings.BatterySaverThreshold != 80 {
		t.Fatalf("unexpected defaults: %+v", settings)
	}

	updated, err := store.UpdateSettings(ctx, UpdateSettingsDTO{
		ThemeMode:             stringPtr("light"),
		ForceLiteMode:         boolPtr(true),
		BatterySaverThreshold: intPtr(75),
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if updated.ThemeMode != "light" || !updated.ForceLiteMode || updated.BatterySaverThreshold != 75 {
		t.Fatalf("fields not applied: %+v", updated)
	}
	if updated.InstanceName != "Phonix Server" || updated.CustomPollIntervalMS != 0 {
		t.Fatalf("untouched fields changed: %+v", updated)
	}

	// Violação de CHECK deve propagar erro de banco sem corromper o singleton.
	overflow := 150
	if _, err := store.UpdateSettings(ctx, UpdateSettingsDTO{BatterySaverThreshold: &overflow}); err == nil {
		t.Fatal("expected CHECK violation for threshold 150")
	}
	reloaded, err := store.GetSettings(ctx)
	if err != nil || reloaded.BatterySaverThreshold != 75 {
		t.Fatalf("settings corrupted after failed update: %v, %+v", err, reloaded)
	}
}

// TestDatabaseLockedMapping segura uma escrita em uma conexão e força
// SQLITE_BUSY em outra com busy_timeout curto, validando o sentinela.
func TestDatabaseLockedMapping(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO shortcuts (title, url) VALUES ('holder', 'http://local')"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil {
			t.Errorf("rollback holder: %v", err)
		}
	}()

	// Conexão crua com timeout mínimo para disparar contenção de imediato.
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(50)")
	raw, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: dbFilenameOf(t, store), RawQuery: query.Encode()}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := raw.Close(); err != nil {
			t.Error(err)
		}
	}()
	_, writeErr := raw.ExecContext(ctx, "INSERT INTO shortcuts (title, url) VALUES ('blocked', 'http://local')")
	if writeErr == nil {
		t.Fatal("expected busy error from locked database")
	}
	if mapped := queryErr("probe", writeErr); !errors.Is(mapped, ErrDatabaseLocked) {
		t.Fatalf("lock not mapped to sentinel: %v", mapped)
	}
}

// dbFilenameOf registra o caminho real do arquivo usado pelo Store de teste.
func dbFilenameOf(t *testing.T, store *Store) string {
	t.Helper()
	var path string
	if err := store.db.QueryRow("PRAGMA database_list").Scan(new(int), new(string), &path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPoolConcurrentAccess(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := store.ListShortcuts(ctx); err != nil {
				failures <- fmt.Errorf("reader %d: %w", i, err)
				return
			}
			if _, err := store.GetSettings(ctx); err != nil {
				failures <- fmt.Errorf("settings reader %d: %w", i, err)
				return
			}
			created, err := store.CreateShortcut(ctx, CreateShortcutDTO{Title: fmt.Sprintf("s%d", i), URL: "http://local"})
			if err != nil {
				failures <- fmt.Errorf("writer %d: %w", i, err)
				return
			}
			if err := store.DeleteShortcut(ctx, created.ID); err != nil {
				failures <- fmt.Errorf("deleter %d: %w", i, err)
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}
