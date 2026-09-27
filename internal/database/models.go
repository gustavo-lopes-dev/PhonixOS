package database

import (
	"database/sql"
	"fmt"
	"time"
)

// Store encapsula o *sql.DB compartilhado inicializado por InitDB e concentra
// os repositórios de atalhos, layout e configurações.
type Store struct {
	db *sql.DB
}

// NewStore vincula os repositórios à conexão já configurada por InitDB.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Shortcut representa a entidade persistida no SQLite (contracts.md §3.4).
type Shortcut struct {
	ID           int64     `db:"id" json:"id"`
	Title        string    `db:"title" json:"title"`
	URL          string    `db:"url" json:"url"`
	IconURL      *string   `db:"icon_url" json:"icon_url"`
	Category     string    `db:"category" json:"category"`
	DisplayOrder int       `db:"display_order" json:"display_order"`
	IsPinned     bool      `db:"is_pinned" json:"is_pinned"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

// LayoutCard representa a posição e dimensão dos cards do painel.
type LayoutCard struct {
	ID        int64     `db:"id" json:"id"`
	CardKey   string    `db:"card_key" json:"card_key"`
	PosX      int       `db:"pos_x" json:"pos_x"`
	PosY      int       `db:"pos_y" json:"pos_y"`
	Width     int       `db:"width" json:"width"`
	Height    int       `db:"height" json:"height"`
	MinWidth  int       `db:"min_width" json:"min_width"`
	MinHeight int       `db:"min_height" json:"min_height"`
	IsVisible bool      `db:"is_visible" json:"is_visible"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// DashboardSettings representa a configuração do servidor (singleton id = 1).
type DashboardSettings struct {
	ID                    int       `db:"id" json:"id"`
	InstanceName          string    `db:"instance_name" json:"instance_name"`
	ThemeMode             string    `db:"theme_mode" json:"theme_mode"`
	ForceLiteMode         bool      `db:"force_lite_mode" json:"force_lite_mode"`
	CustomPollIntervalMS  int       `db:"custom_poll_interval_ms" json:"custom_poll_interval_ms"`
	BatterySaverThreshold int       `db:"battery_saver_threshold" json:"battery_saver_threshold"`
	CreatedAt             time.Time `db:"created_at" json:"created_at"`
	UpdatedAt             time.Time `db:"updated_at" json:"updated_at"`
}

// DTOs de Entrada para Validação de Requisições REST (contracts.md §3.4).

type CreateShortcutDTO struct {
	Title        string  `json:"title" validate:"required,min=1,max=64"`
	URL          string  `json:"url" validate:"required,url"`
	IconURL      *string `json:"icon_url" validate:"omitempty,url"`
	Category     *string `json:"category" validate:"omitempty,max=32"`
	DisplayOrder *int    `json:"display_order"`
	IsPinned     *bool   `json:"is_pinned"`
}

type UpdateShortcutDTO struct {
	Title        *string `json:"title" validate:"omitempty,min=1,max=64"`
	URL          *string `json:"url" validate:"omitempty,url"`
	IconURL      *string `json:"icon_url" validate:"omitempty,url"`
	Category     *string `json:"category" validate:"omitempty,max=32"`
	DisplayOrder *int    `json:"display_order"`
	IsPinned     *bool   `json:"is_pinned"`
}

type UpdateLayoutCardDTO struct {
	CardKey   string `json:"card_key" validate:"required"`
	PosX      int    `json:"pos_x" validate:"gte=0"`
	PosY      int    `json:"pos_y" validate:"gte=0"`
	Width     int    `json:"width" validate:"gt=0"`
	Height    int    `json:"height" validate:"gt=0"`
	IsVisible bool   `json:"is_visible"`
}

type UpdateSettingsDTO struct {
	InstanceName          *string `json:"instance_name" validate:"omitempty,min=1,max=32"`
	ThemeMode             *string `json:"theme_mode" validate:"omitempty,oneof=dark light system"`
	ForceLiteMode         *bool   `json:"force_lite_mode"`
	CustomPollIntervalMS  *int    `json:"custom_poll_interval_ms" validate:"omitempty,gte=0,lte=60000"`
	BatterySaverThreshold *int    `json:"battery_saver_threshold" validate:"omitempty,gte=50,lte=100"`
}

// rowScanner abstrai sql.Row e sql.Rows para os helpers de scan.
type rowScanner interface {
	Scan(dest ...any) error
}

// parseDBTime converte o texto gerado por strftime('%Y-%m-%dT%H:%M:%SZ', 'now').
func parseDBTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse sqlite timestamp %q: %w", value, err)
	}
	return parsed, nil
}

// boolToInt normaliza booleanos para o CHECK (coluna IN (0, 1)) do DDL.
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// nullString traduz ponteiros opcionais para NULL do SQLite.
func nullString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
