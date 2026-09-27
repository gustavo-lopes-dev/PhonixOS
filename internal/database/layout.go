package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const layoutCardColumns = "id, card_key, pos_x, pos_y, width, height, min_width, min_height, is_visible, updated_at"

// ListLayoutCards devolve todos os cards do mosaico ordenados por id ASC.
func (s *Store) ListLayoutCards(ctx context.Context) (cards []LayoutCard, err error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+layoutCardColumns+" FROM layout_cards ORDER BY id ASC")
	if err != nil {
		return nil, queryErr("list layout cards", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = queryErr("list layout cards (close)", closeErr)
		}
	}()

	cards = []LayoutCard{}
	for rows.Next() {
		card, err := scanLayoutCard(rows)
		if err != nil {
			return nil, queryErr("list layout cards", err)
		}
		cards = append(cards, *card)
	}
	if err := rows.Err(); err != nil {
		return nil, queryErr("list layout cards", err)
	}
	return cards, nil
}

// GetLayoutCard localiza um card pela chave ou devolve ErrCardNotFound.
func (s *Store) GetLayoutCard(ctx context.Context, cardKey string) (*LayoutCard, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+layoutCardColumns+" FROM layout_cards WHERE card_key = ?", cardKey)
	card, err := scanLayoutCard(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %q", ErrCardNotFound, cardKey)
		}
		return nil, queryErr("get layout card", err)
	}
	return card, nil
}

// UpdateLayoutCards aplica o lote em uma única transação: qualquer card_key
// desconhecida (ErrCardNotFound) ou falha de execução reverte todas as
// alterações. Em sucesso, devolve a listagem atualizada.
func (s *Store) UpdateLayoutCards(ctx context.Context, cards []UpdateLayoutCardDTO) (updated []LayoutCard, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, queryErr("update layout cards (begin)", err)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("update layout cards (rollback): %w", rollbackErr))
			}
		}
	}()

	for _, card := range cards {
		result, err := tx.ExecContext(ctx,
			"UPDATE layout_cards SET pos_x = ?, pos_y = ?, width = ?, height = ?, is_visible = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE card_key = ?",
			card.PosX, card.PosY, card.Width, card.Height, boolToInt(card.IsVisible), card.CardKey)
		if err != nil {
			return nil, queryErr("update layout card", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, queryErr("update layout card", err)
		}
		if affected == 0 {
			return nil, fmt.Errorf("%w: %q", ErrCardNotFound, card.CardKey)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, queryErr("update layout cards (commit)", err)
	}
	return s.ListLayoutCards(ctx)
}

func scanLayoutCard(row rowScanner) (*LayoutCard, error) {
	var card LayoutCard
	var visible int64
	var updatedAt string
	if err := row.Scan(&card.ID, &card.CardKey, &card.PosX, &card.PosY, &card.Width, &card.Height,
		&card.MinWidth, &card.MinHeight, &visible, &updatedAt); err != nil {
		return nil, err
	}
	card.IsVisible = visible != 0

	var err error
	if card.UpdatedAt, err = parseDBTime(updatedAt); err != nil {
		return nil, err
	}
	return &card, nil
}
