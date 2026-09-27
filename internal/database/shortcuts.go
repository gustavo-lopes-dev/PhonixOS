package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// defaultShortcutCategory espelha o DEFAULT 'Geral' do DDL da migration inicial.
const defaultShortcutCategory = "Geral"

const shortcutColumns = "id, title, url, icon_url, category, display_order, is_pinned, created_at, updated_at"

// ListShortcuts devolve todos os atalhos em ordenação determinística:
// display_order ASC, id ASC.
func (s *Store) ListShortcuts(ctx context.Context) (shortcuts []Shortcut, err error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+shortcutColumns+" FROM shortcuts ORDER BY display_order ASC, id ASC")
	if err != nil {
		return nil, queryErr("list shortcuts", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = queryErr("list shortcuts (close)", closeErr)
		}
	}()

	shortcuts = []Shortcut{}
	for rows.Next() {
		shortcut, err := scanShortcut(rows)
		if err != nil {
			return nil, queryErr("list shortcuts", err)
		}
		shortcuts = append(shortcuts, *shortcut)
	}
	if err := rows.Err(); err != nil {
		return nil, queryErr("list shortcuts", err)
	}
	return shortcuts, nil
}

// GetShortcut localiza um atalho pelo ID ou devolve ErrShortcutNotFound.
func (s *Store) GetShortcut(ctx context.Context, id int64) (*Shortcut, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+shortcutColumns+" FROM shortcuts WHERE id = ?", id)
	shortcut, err := scanShortcut(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: id %d", ErrShortcutNotFound, id)
		}
		return nil, queryErr("get shortcut", err)
	}
	return shortcut, nil
}

// CreateShortcut insere um atalho aplicando os defaults do DDL quando os
// campos opcionais do DTO estão ausentes, e devolve a entidade persistida.
func (s *Store) CreateShortcut(ctx context.Context, dto CreateShortcutDTO) (*Shortcut, error) {
	category := defaultShortcutCategory
	if dto.Category != nil && *dto.Category != "" {
		category = *dto.Category
	}
	displayOrder := 0
	if dto.DisplayOrder != nil {
		displayOrder = *dto.DisplayOrder
	}
	isPinned := false
	if dto.IsPinned != nil {
		isPinned = *dto.IsPinned
	}

	result, err := s.db.ExecContext(ctx,
		"INSERT INTO shortcuts (title, url, icon_url, category, display_order, is_pinned) VALUES (?, ?, ?, ?, ?, ?)",
		dto.Title, dto.URL, nullString(dto.IconURL), category, displayOrder, boolToInt(isPinned))
	if err != nil {
		return nil, queryErr("create shortcut", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, queryErr("create shortcut", err)
	}
	return s.GetShortcut(ctx, id)
}

// UpdateShortcut altera somente os campos presentes em uma única instrução,
// preservando atualizações concorrentes de outros campos. IconURLPresent permite
// limpar icon_url com JSON null sem confundi-lo com um campo omitido.
func (s *Store) UpdateShortcut(ctx context.Context, id int64, dto UpdateShortcutDTO) (*Shortcut, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE shortcuts SET title = COALESCE(?, title), url = COALESCE(?, url),
		 icon_url = CASE WHEN ? THEN ? ELSE icon_url END,
		 category = COALESCE(?, category), display_order = COALESCE(?, display_order),
		 is_pinned = COALESCE(?, is_pinned), updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
		 WHERE id = ?`,
		dto.Title, dto.URL, dto.IconURLPresent || dto.IconURL != nil, nullString(dto.IconURL),
		dto.Category, dto.DisplayOrder, dto.IsPinned, id)
	if err != nil {
		return nil, queryErr("update shortcut", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, queryErr("update shortcut", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("%w: id %d", ErrShortcutNotFound, id)
	}
	return s.GetShortcut(ctx, id)
}

// DeleteShortcut remove um atalho; inexistência devolve ErrShortcutNotFound.
func (s *Store) DeleteShortcut(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM shortcuts WHERE id = ?", id)
	if err != nil {
		return queryErr("delete shortcut", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return queryErr("delete shortcut", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: id %d", ErrShortcutNotFound, id)
	}
	return nil
}

func scanShortcut(row rowScanner) (*Shortcut, error) {
	var shortcut Shortcut
	var iconURL sql.NullString
	var pinned int64
	var createdAt, updatedAt string
	if err := row.Scan(&shortcut.ID, &shortcut.Title, &shortcut.URL, &iconURL,
		&shortcut.Category, &shortcut.DisplayOrder, &pinned, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if iconURL.Valid {
		shortcut.IconURL = &iconURL.String
	}
	shortcut.IsPinned = pinned != 0

	var err error
	if shortcut.CreatedAt, err = parseDBTime(createdAt); err != nil {
		return nil, err
	}
	if shortcut.UpdatedAt, err = parseDBTime(updatedAt); err != nil {
		return nil, err
	}
	return &shortcut, nil
}
