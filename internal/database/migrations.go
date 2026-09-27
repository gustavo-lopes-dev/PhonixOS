package database

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func RunMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	migrationNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		migrationNames = append(migrationNames, entry.Name())
	}
	sort.Strings(migrationNames)

	for _, name := range migrationNames {
		statement, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", name, err)
		}
		if err := applyMigration(db, name, string(statement)); err != nil {
			return err
		}
	}

	return nil
}

// applyMigration registra a versão na mesma transação do SQL. A inserção
// antecede o schema para serializar inicializações concorrentes entre processos;
// em caso de falha, ambos são revertidos.
func applyMigration(db *sql.DB, name, statement string) (err error) {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %q: %w", name, err)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("rollback migration %q: %w", name, rollbackErr))
			}
		}
	}()

	result, err := tx.Exec("INSERT OR IGNORE INTO schema_migrations (version) VALUES (?)", name)
	if err != nil {
		return fmt.Errorf("record migration %q: %w", name, err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check migration %q: %w", name, err)
	}
	if inserted != 0 {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply migration %q: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %q: %w", name, err)
	}
	return nil
}
