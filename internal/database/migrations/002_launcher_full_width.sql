-- Lançador de serviços em largura total (6 colunas), como no mockup da Fase 3.
-- Só altera instalações que ainda mantêm o card no valor do seed de 001_initial_schema.sql,
-- preservando layouts personalizados.
UPDATE layout_cards
SET width = 6, updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
WHERE card_key = 'app_launcher' AND pos_x = 0 AND pos_y = 2 AND width = 4 AND height = 4;
