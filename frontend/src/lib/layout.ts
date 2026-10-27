// Ordenação e reempacotamento da grade de 6 colunas (contracts.md §5.3).
// Funções puras: a UI reordena chaves e este módulo devolve o lote completo do `PUT /api/v1/layout`.

import type { LayoutCard, UpdateLayoutCardDTO } from '../types/dashboard.ts';

export const GRID_COLUMNS = 6;

/** `card_key` canônicas do seed (contracts.md §2); outras são preservadas mas não renderizadas. */
export const CARD_KEYS = ['metrics_cpu', 'metrics_memory', 'metrics_battery', 'app_launcher'] as const;
export type CardKey = (typeof CARD_KEYS)[number];

export function isCardKey(key: string): key is CardKey {
	return (CARD_KEYS as readonly string[]).includes(key);
}

/** Ordem visual canônica: linha (`pos_y`), coluna (`pos_x`) e `id` no empate. Não altera a entrada. */
export function sortCards<T extends Pick<LayoutCard, 'id' | 'pos_x' | 'pos_y'>>(cards: readonly T[]): T[] {
	return [...cards].sort((a, b) => a.pos_y - b.pos_y || a.pos_x - b.pos_x || a.id - b.id);
}

/** Move o item de `from` para `to` (índices da lista resultante). Não altera a entrada. */
export function moveItem<T>(list: readonly T[], from: number, to: number): T[] {
	const next = [...list];
	const [item] = next.splice(from, 1);
	if (item === undefined) return next;
	next.splice(Math.max(0, Math.min(to, next.length)), 0, item);
	return next;
}

function clampWidth(card: LayoutCard): number {
	return Math.min(GRID_COLUMNS, Math.max(card.width, card.min_width, 1));
}

function clampHeight(card: LayoutCard): number {
	return Math.max(card.height, card.min_height, 1);
}

/**
 * Reempacota os cards de `order` (visíveis, nessa ordem) em linhas de 6 colunas: quebra quando
 * `pos_x + width > 6` e avança `pos_y` pela maior `height` da linha. Os cards fora de `order`
 * ficam ocultos, anexados em linhas seguintes na ordem canônica, mantendo `width`/`height`.
 * Garante `width >= min_width`, `height >= min_height` e `pos_x + width <= 6`; chaves de `order`
 * desconhecidas ou repetidas são ignoradas. Devolve **todos** os cards com todos os campos do DTO.
 */
export function packLayout(order: readonly string[], cards: readonly LayoutCard[]): UpdateLayoutCardDTO[] {
	const byKey = new Map(cards.map((card) => [card.card_key, card]));
	const visible: LayoutCard[] = [];
	const seen = new Set<string>();
	for (const key of order) {
		const card = byKey.get(key);
		if (card === undefined || seen.has(key)) continue;
		seen.add(key);
		visible.push(card);
	}
	const hidden = sortCards(cards.filter((card) => !seen.has(card.card_key)));

	const result: UpdateLayoutCardDTO[] = [];
	let x = 0;
	let y = 0;
	let rowHeight = 0;
	const newRow = () => {
		y += rowHeight;
		x = 0;
		rowHeight = 0;
	};
	const place = (card: LayoutCard, isVisible: boolean) => {
		const width = clampWidth(card);
		const height = clampHeight(card);
		if (x + width > GRID_COLUMNS) newRow();
		result.push({ card_key: card.card_key, pos_x: x, pos_y: y, width, height, is_visible: isVisible });
		x += width;
		rowHeight = Math.max(rowHeight, height);
	};

	for (const card of visible) place(card, true);
	if (x > 0) newRow();
	for (const card of hidden) place(card, false);
	return result;
}
