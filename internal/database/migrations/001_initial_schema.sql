-- Habilitação explícita de chaves estrangeiras
PRAGMA foreign_keys = ON;

-- Tabela de Atalhos do Lançador de Aplicações
CREATE TABLE IF NOT EXISTS shortcuts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    icon_url TEXT,
    category TEXT NOT NULL DEFAULT 'Geral',
    display_order INTEGER NOT NULL DEFAULT 0,
    is_pinned BOOLEAN NOT NULL DEFAULT 0 CHECK (is_pinned IN (0, 1)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_shortcuts_display_order ON shortcuts (display_order ASC);
CREATE INDEX IF NOT EXISTS idx_shortcuts_category ON shortcuts (category);

-- Tabela de Disposição dos Cards do Dashboard (Grid Mosaic)
CREATE TABLE IF NOT EXISTS layout_cards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    card_key TEXT NOT NULL UNIQUE,
    pos_x INTEGER NOT NULL DEFAULT 0,
    pos_y INTEGER NOT NULL DEFAULT 0,
    width INTEGER NOT NULL DEFAULT 2,
    height INTEGER NOT NULL DEFAULT 2,
    min_width INTEGER NOT NULL DEFAULT 1,
    min_height INTEGER NOT NULL DEFAULT 1,
    is_visible BOOLEAN NOT NULL DEFAULT 1 CHECK (is_visible IN (0, 1)),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Tabela de Configurações do Painel e Preferências de Sistema (Singleton: ID = 1)
CREATE TABLE IF NOT EXISTS dashboard_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    instance_name TEXT NOT NULL DEFAULT 'Phonix Server',
    theme_mode TEXT NOT NULL DEFAULT 'dark' CHECK (theme_mode IN ('dark', 'light', 'system')),
    force_lite_mode BOOLEAN NOT NULL DEFAULT 0 CHECK (force_lite_mode IN (0, 1)),
    custom_poll_interval_ms INTEGER NOT NULL DEFAULT 0,
    battery_saver_threshold INTEGER NOT NULL DEFAULT 80 CHECK (battery_saver_threshold BETWEEN 50 AND 100),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Inserção do registro singleton padrão
INSERT OR IGNORE INTO dashboard_settings (
    id, instance_name, theme_mode, force_lite_mode, custom_poll_interval_ms, battery_saver_threshold
) VALUES (1, 'Phonix Server', 'dark', 0, 0, 80);

-- Seed do catálogo canônico de cards do dashboard (grade 6 colunas)
INSERT OR IGNORE INTO layout_cards (
    card_key, pos_x, pos_y, width, height, min_width, min_height, is_visible
) VALUES
    ('metrics_cpu',     0, 0, 2, 2, 2, 2, 1),
    ('metrics_memory',  2, 0, 2, 2, 2, 2, 1),
    ('metrics_battery', 4, 0, 2, 2, 2, 2, 1),
    ('app_launcher',    0, 2, 4, 4, 2, 2, 1);
