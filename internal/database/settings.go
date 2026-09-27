package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// settingsSingletonID fixa a linha única de configurações criada pelo seed.
const settingsSingletonID = 1

// GetSettings devolve a configuração singleton (id = 1) do painel.
func (s *Store) GetSettings(ctx context.Context) (*DashboardSettings, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, instance_name, theme_mode, force_lite_mode, custom_poll_interval_ms, battery_saver_threshold, created_at, updated_at FROM dashboard_settings WHERE id = ?",
		settingsSingletonID)
	settings, err := scanSettings(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get settings: singleton row id = %d missing", settingsSingletonID)
		}
		return nil, queryErr("get settings", err)
	}
	return settings, nil
}

// UpdateSettings altera apenas os campos presentes em uma única instrução
// atômica, sem sobrescrever PATCHes concorrentes de outros campos.
func (s *Store) UpdateSettings(ctx context.Context, dto UpdateSettingsDTO) (*DashboardSettings, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE dashboard_settings SET instance_name = COALESCE(?, instance_name),
		 theme_mode = COALESCE(?, theme_mode), force_lite_mode = COALESCE(?, force_lite_mode),
		 custom_poll_interval_ms = COALESCE(?, custom_poll_interval_ms),
		 battery_saver_threshold = COALESCE(?, battery_saver_threshold),
		 updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now') WHERE id = ?`,
		dto.InstanceName, dto.ThemeMode, dto.ForceLiteMode,
		dto.CustomPollIntervalMS, dto.BatterySaverThreshold, settingsSingletonID)
	if err != nil {
		return nil, queryErr("update settings", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, queryErr("update settings", err)
	}
	if affected == 0 {
		return nil, fmt.Errorf("update settings: singleton row id = %d missing", settingsSingletonID)
	}
	return s.GetSettings(ctx)
}

func scanSettings(row rowScanner) (*DashboardSettings, error) {
	var settings DashboardSettings
	var forceLite int64
	var createdAt, updatedAt string
	if err := row.Scan(&settings.ID, &settings.InstanceName, &settings.ThemeMode, &forceLite,
		&settings.CustomPollIntervalMS, &settings.BatterySaverThreshold, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	settings.ForceLiteMode = forceLite != 0

	var err error
	if settings.CreatedAt, err = parseDBTime(createdAt); err != nil {
		return nil, err
	}
	if settings.UpdatedAt, err = parseDBTime(updatedAt); err != nil {
		return nil, err
	}
	return &settings, nil
}
