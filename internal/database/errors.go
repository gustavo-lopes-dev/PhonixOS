package database

import (
	"errors"
	"fmt"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Erros sentinela da camada de persistência. Consumidores devem compará-los
// com errors.Is, pois podem vir embrulhados com contexto (id, chave, operação).
var (
	ErrShortcutNotFound = errors.New("shortcut not found")
	ErrCardNotFound     = errors.New("layout card not found")
	ErrDatabaseLocked   = errors.New("database is locked")
)

// queryErr contextualiza falhas de execução e contém a contenção de trava do
// SQLite (SQLITE_BUSY/SQLITE_LOCKED, inclusive códigos estendidos) no sentinela
// ErrDatabaseLocked.
func queryErr(op string, err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() & 0xFF {
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
			return fmt.Errorf("%s: %w", op, ErrDatabaseLocked)
		}
	}
	return fmt.Errorf("%s: %w", op, err)
}
