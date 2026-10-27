<script lang="ts">
	// Grade do painel (handoff §A "Grid", §B, §C): 6 colunas no desktop (≥ 1024 px), coluna única
	// no mobile, ordem por (`pos_y`, `pos_x`). Na edição mantém um rascunho local (nada é emitido
	// antes de "Salvar layout"): desktop reordena arrastando pela alça (Pointer Events) ou com as
	// setas; mobile usa ↑/↓. Emite o lote completo de `UpdateLayoutCardDTO`; não chama a API.
	import { tick, untrack, type Snippet } from 'svelte';
	import { t, type MessageKey } from '#lib/i18n/index.ts';
	import { GRID_COLUMNS, isCardKey, moveItem, packLayout, sortCards, type CardKey } from '#lib/layout.ts';
	import type { LayoutCard, UpdateLayoutCardDTO } from '../../types/dashboard.ts';

	interface Props {
		cards: readonly LayoutCard[];
		editing: boolean;
		/** Conteúdo de cada card: `full` no painel, `summary` (uma linha) na edição. */
		card: Snippet<[key: CardKey, mode: 'full' | 'summary']>;
		onsave: (cards: UpdateLayoutCardDTO[]) => void;
		oncancel: () => void;
	}

	let { cards, editing, card, onsave, oncancel }: Props = $props();

	const nameKey: Record<CardKey, MessageKey> = {
		metrics_cpu: 'metrics.cpuTitle',
		metrics_memory: 'metrics.memoryTitle',
		metrics_battery: 'metrics.batteryTitle',
		app_launcher: 'launcher.title'
	};

	/** Distância mínima (px) para o ponteiro iniciar o arraste. */
	const DRAG_THRESHOLD = 4;

	interface Draft {
		order: CardKey[];
		hidden: CardKey[];
	}

	interface PageRect {
		top: number;
		right: number;
		bottom: number;
		left: number;
	}

	interface DragState {
		key: CardKey;
		pointerId: number;
		from: number;
		to: number;
		active: boolean;
		startX: number;
		startY: number;
		clientX: number;
		clientY: number;
		offsetX: number;
		offsetY: number;
		width: number;
		height: number;
		/** Retângulos (coordenadas de página) dos demais cards no início do arraste. */
		rects: PageRect[];
	}

	function createDraft(source: readonly LayoutCard[]): Draft {
		const draft: Draft = { order: [], hidden: [] };
		for (const item of sortCards(source)) {
			if (!isCardKey(item.card_key)) continue;
			(item.is_visible ? draft.order : draft.hidden).push(item.card_key);
		}
		return draft;
	}

	const spans = $derived(
		new Map(cards.map((item) => [item.card_key, Math.max(1, Math.min(item.width, GRID_COLUMNS))]))
	);
	const visibleKeys = $derived(
		sortCards(cards)
			.filter((item) => item.is_visible)
			.map((item) => item.card_key)
			.filter(isCardKey)
	);

	// Rascunho recriado a cada entrada no modo de edição; reatribuído localmente durante a edição.
	let draft = $derived<Draft | null>(editing ? untrack(() => createDraft(cards)) : null);
	let drag = $state.raw<DragState | null>(null);
	let announcement = $state('');

	const displayOrder = $derived(
		draft && drag?.active ? moveItem(draft.order, drag.from, drag.to) : (draft?.order ?? [])
	);

	const itemEls: Partial<Record<CardKey, HTMLElement>> = {};
	const handleEls: Partial<Record<CardKey, HTMLButtonElement>> = {};
	const upEls: Partial<Record<CardKey, HTMLButtonElement>> = {};
	const downEls: Partial<Record<CardKey, HTMLButtonElement>> = {};

	const spanOf = (key: CardKey) => spans.get(key) ?? 1;
	const nameOf = (key: CardKey) => $t(nameKey[key]);

	function announceMove(key: CardKey, index: number) {
		announcement = $t('layout.moved', { name: nameOf(key), n: index + 1 });
	}

	/** Move o card `delta` posições; devolve o novo índice ou `null` se não mudou. */
	function moveBy(key: CardKey, delta: number): number | null {
		if (!draft) return null;
		const from = draft.order.indexOf(key);
		const to = from + delta;
		if (from < 0 || to < 0 || to >= draft.order.length) return null;
		draft = { ...draft, order: moveItem(draft.order, from, to) };
		announceMove(key, to);
		return to;
	}

	async function onHandleKey(event: KeyboardEvent, key: CardKey) {
		const delta =
			event.key === 'ArrowUp' || event.key === 'ArrowLeft'
				? -1
				: event.key === 'ArrowDown' || event.key === 'ArrowRight'
					? 1
					: 0;
		if (delta === 0) return;
		event.preventDefault();
		if (moveBy(key, delta) === null) return;
		await tick();
		handleEls[key]?.focus();
	}

	async function onStep(key: CardKey, delta: -1 | 1) {
		const index = moveBy(key, delta);
		if (index === null || !draft) return;
		await tick();
		const atEdge = delta < 0 ? index === 0 : index === draft.order.length - 1;
		(delta < 0 ? (atEdge ? downEls : upEls) : atEdge ? upEls : downEls)[key]?.focus();
	}

	function hide(key: CardKey) {
		if (!draft) return;
		draft = { order: draft.order.filter((item) => item !== key), hidden: [...draft.hidden, key] };
	}

	function show(key: CardKey) {
		if (!draft) return;
		draft = { order: [...draft.order, key], hidden: draft.hidden.filter((item) => item !== key) };
		announceMove(key, draft.order.length - 1);
	}

	function save() {
		if (!draft) return;
		// Cards com chave desconhecida não são renderizados, mas seguem visíveis no lote.
		const unknownVisible = sortCards(cards)
			.filter((item) => item.is_visible && !isCardKey(item.card_key))
			.map((item) => item.card_key);
		onsave(packLayout([...draft.order, ...unknownVisible], cards));
	}

	function startDrag(event: PointerEvent, key: CardKey) {
		if (!draft || event.button !== 0 || drag) return;
		const element = itemEls[key];
		if (!element) return;
		event.preventDefault();
		const rect = element.getBoundingClientRect();
		const rects = draft.order
			.filter((item) => item !== key)
			.map((item) => {
				const box = itemEls[item]?.getBoundingClientRect();
				return box
					? {
							top: box.top + window.scrollY,
							right: box.right + window.scrollX,
							bottom: box.bottom + window.scrollY,
							left: box.left + window.scrollX
						}
					: { top: Infinity, right: Infinity, bottom: Infinity, left: Infinity };
			});
		const from = draft.order.indexOf(key);
		drag = {
			key,
			pointerId: event.pointerId,
			from,
			to: from,
			active: false,
			startX: event.pageX,
			startY: event.pageY,
			clientX: event.clientX,
			clientY: event.clientY,
			offsetX: event.clientX - rect.left,
			offsetY: event.clientY - rect.top,
			width: rect.width,
			height: rect.height,
			rects
		};
	}

	/** Índice de inserção: quantos cards ficam antes do ponteiro na ordem de leitura. */
	function dropIndex(pageX: number, pageY: number, rects: readonly PageRect[]): number {
		let index = 0;
		for (const rect of rects) {
			const before = pageY > rect.bottom || (pageY >= rect.top && pageX > (rect.left + rect.right) / 2);
			if (before) index += 1;
		}
		return index;
	}

	function onPointerMove(event: PointerEvent) {
		if (!drag || event.pointerId !== drag.pointerId) return;
		const active =
			drag.active || Math.hypot(event.pageX - drag.startX, event.pageY - drag.startY) > DRAG_THRESHOLD;
		drag = {
			...drag,
			active,
			clientX: event.clientX,
			clientY: event.clientY,
			to: active ? dropIndex(event.pageX, event.pageY, drag.rects) : drag.to
		};
	}

	async function onPointerUp(event: PointerEvent) {
		if (!drag || event.pointerId !== drag.pointerId) return;
		const { key, from, to, active } = drag;
		drag = null;
		if (!draft || !active) return;
		if (to !== from) {
			draft = { ...draft, order: moveItem(draft.order, from, to) };
			announceMove(key, to);
		}
		await tick();
		handleEls[key]?.focus();
	}

	function cancelDrag() {
		drag = null;
	}

	function onWindowKey(event: KeyboardEvent) {
		if (drag && event.key === 'Escape') {
			event.preventDefault();
			cancelDrag();
		}
	}
</script>

<svelte:window
	onpointermove={onPointerMove}
	onpointerup={onPointerUp}
	onpointercancel={cancelDrag}
	onkeydown={onWindowKey}
	onblur={cancelDrag}
/>

{#snippet grip()}
	<span class="grid grid-cols-[3px_3px] gap-[3px]" aria-hidden="true">
		{#each { length: 6 }, dot (dot)}
			<span class="size-[3px] rounded-full bg-current"></span>
		{/each}
	</span>
{/snippet}

{#if draft}
	<p class="sr-only" aria-live="polite">{announcement}</p>

	<!-- Edição desktop -->
	<div class="flex flex-col gap-6 max-lg:hidden">
		<div class="flex items-center gap-4 rounded-[14px] border border-accent-border bg-accent-soft py-3.5 pr-4 pl-5">
			<div class="flex flex-1 flex-col gap-1">
				<span class="text-[14px] leading-[1.2] font-semibold">{$t('layout.editingTitle')}</span>
				<span class="text-body leading-[1.4] text-muted">
					{$t('layout.hintDragStart')}
					<b class="font-semibold text-text">{$t('layout.move')}</b>
					{$t('layout.hintDragEnd')}
					<b class="font-semibold text-text">{$t('common.hide')}</b>
					{$t('layout.hintHide')}
				</span>
			</div>
			<button
				type="button"
				class="flex h-[38px] flex-none items-center rounded-[9px] border border-border bg-inner px-4 text-button"
				onclick={oncancel}
			>
				{$t('common.cancel')}
			</button>
			<button
				type="button"
				class="flex h-[38px] flex-none items-center rounded-[9px] bg-btn-primary px-4 text-button text-btn-primary-text"
				onclick={save}
			>
				{$t('layout.saveLayout')}
			</button>
		</div>

		<div class="flex flex-wrap items-center gap-3 rounded-[14px] border border-dashed border-border py-2.5 pr-3 pl-[18px]">
			<span class="text-body leading-none font-medium text-muted">
				{$t('layout.hiddenCards', { count: draft.hidden.length })}
			</span>
			{#each draft.hidden as key (key)}
				<span class="surface-inner inline-flex h-9 items-center gap-2.5 rounded-[9px] pr-1.5 pl-3 text-body leading-none font-medium">
					{nameOf(key)}
					<button
						type="button"
						class="flex h-[26px] items-center rounded-badge bg-accent-soft px-2.5 text-meta leading-none font-semibold text-accent"
						aria-label={$t('layout.showCard', { name: nameOf(key) })}
						onclick={() => show(key)}
					>
						{$t('common.show')}
					</button>
				</span>
			{:else}
				<span class="text-body leading-none text-muted">{$t('layout.noHiddenCards')}</span>
			{/each}
		</div>

		<div class="grid grid-cols-6 gap-5">
			{#each displayOrder as key, index (key)}
				<div bind:this={itemEls[key]} class="min-w-0 col-span-(--span)" style:--span={spanOf(key)}>
					{#if drag?.active && drag.key === key}
						<div
							class="grid h-full min-h-[212px] place-items-center rounded-card border-2 border-dashed border-border bg-accent-soft text-center"
						>
							<span class="flex flex-col gap-1 text-body leading-[1.5] font-medium text-muted">
								{$t('layout.dropHere')}
								<span class="num text-meta leading-none font-normal">{$t('layout.position', { n: index + 1 })}</span>
							</span>
						</div>
					{:else}
						<section
							aria-label={nameOf(key)}
							class="flex h-full min-h-[212px] flex-col gap-[18px] rounded-card border-[1.5px] border-dashed border-accent bg-card px-4 pt-3.5 pb-5 [backdrop-filter:var(--surface-blur)]"
						>
							<div class="flex items-center justify-between gap-2">
								<button
									bind:this={handleEls[key]}
									type="button"
									class="flex h-8 cursor-grab touch-none items-center gap-2 rounded-small border border-border bg-inner px-2.5 text-label leading-none font-medium select-none"
									aria-label={$t('layout.handleLabel', { name: nameOf(key), n: index + 1 })}
									onpointerdown={(event) => startDrag(event, key)}
									onkeydown={(event) => onHandleKey(event, key)}
								>
									{@render grip()}
									{$t('layout.move')}
								</button>
								<button
									type="button"
									class="flex h-8 items-center rounded-small border border-border px-2.5 text-label leading-none font-medium"
									aria-label={$t('layout.hideCard', { name: nameOf(key) })}
									onclick={() => hide(key)}
								>
									{$t('common.hide')}
								</button>
							</div>
							{@render card(key, 'summary')}
							<span class="num mt-auto text-meta leading-none text-muted">
								{$t('layout.position', { n: index + 1 })} · {$t('layout.columns', { count: spanOf(key) })}
							</span>
						</section>
					{/if}
				</div>
			{/each}
		</div>
	</div>

	{#if drag?.active}
		<section
			aria-hidden="true"
			class="pointer-events-none fixed z-50 flex min-h-[212px] -rotate-[1.6deg] cursor-grabbing flex-col gap-[18px] rounded-card border-2 border-accent bg-card-solid px-4 pt-3.5 pb-5 shadow-popover"
			style:left="{drag.clientX - drag.offsetX}px"
			style:top="{drag.clientY - drag.offsetY}px"
			style:width="{drag.width}px"
			style:height="{drag.height}px"
		>
			<div class="flex items-center justify-between gap-2">
				<span
					class="flex h-8 items-center gap-2 rounded-small bg-btn-primary px-2.5 text-label leading-none font-semibold text-btn-primary-text"
				>
					{@render grip()}
					{$t('layout.moving')}
				</span>
				<span class="flex h-8 items-center rounded-small border border-border px-2.5 text-label leading-none font-medium">
					{$t('common.hide')}
				</span>
			</div>
			{@render card(drag.key, 'summary')}
			<span class="num mt-auto text-meta leading-none text-muted">
				{$t('layout.positionMove', { from: drag.from + 1, to: drag.to + 1 })}
			</span>
		</section>
	{/if}

	<!-- Edição mobile: ↑/↓, sem arrastar no toque -->
	<div class="flex flex-col gap-3.5 pb-28 lg:hidden">
		<p class="px-0.5 py-1 text-[13.5px] leading-[1.45] text-muted">
			{$t('layout.mobileHintStart')} <b class="text-text">↑</b>
			{$t('layout.and')} <b class="text-text">↓</b>
			{$t('layout.mobileHintMiddle')} <b class="text-text">{$t('common.hide')}</b>
			{$t('layout.mobileHintEnd')}
		</p>
		<h3 class="px-0.5 pt-1.5 text-[11px] leading-none font-semibold tracking-[0.09em] text-muted uppercase">
			{$t('layout.onDashboard', { n: draft.order.length })}
		</h3>
		<ol class="flex flex-col gap-3.5">
			{#each draft.order as key, index (key)}
				<li
					class="flex items-center gap-2 rounded-[14px] border-[1.5px] border-dashed border-accent bg-card py-2.5 pr-2.5 pl-4 [backdrop-filter:var(--surface-blur)]"
				>
					<span class="flex min-w-0 flex-1 flex-col gap-[5px]">
						<span class="text-[14px] leading-[1.1] font-semibold">{nameOf(key)}</span>
						<span class="num text-[11.5px] leading-none text-muted">{$t('layout.position', { n: index + 1 })}</span>
					</span>
					<button
						bind:this={upEls[key]}
						type="button"
						class="grid size-11 flex-none place-items-center rounded-control border border-border bg-inner text-[17px] leading-none font-semibold disabled:opacity-35"
						aria-label={$t('layout.moveUp', { name: nameOf(key) })}
						disabled={index === 0}
						onclick={() => onStep(key, -1)}
					>
						↑
					</button>
					<button
						bind:this={downEls[key]}
						type="button"
						class="grid size-11 flex-none place-items-center rounded-control border border-border bg-inner text-[17px] leading-none font-semibold disabled:opacity-35"
						aria-label={$t('layout.moveDown', { name: nameOf(key) })}
						disabled={index === draft.order.length - 1}
						onclick={() => onStep(key, 1)}
					>
						↓
					</button>
					<button
						type="button"
						class="flex h-11 flex-none items-center rounded-control border border-border px-3 text-button"
						aria-label={$t('layout.hideCard', { name: nameOf(key) })}
						onclick={() => hide(key)}
					>
						{$t('common.hide')}
					</button>
				</li>
			{/each}
		</ol>
		<h3 class="px-0.5 pt-2.5 text-[11px] leading-none font-semibold tracking-[0.09em] text-muted uppercase">
			{$t('layout.hiddenSection', { n: draft.hidden.length })}
		</h3>
		{#each draft.hidden as key (key)}
			<div class="flex items-center gap-2 rounded-[14px] border border-dashed border-border py-2.5 pr-2.5 pl-4">
				<span class="flex min-w-0 flex-1 flex-col gap-[5px]">
					<span class="text-[14px] leading-[1.1] font-semibold text-muted">{nameOf(key)}</span>
					<span class="num text-[11.5px] leading-none text-muted">{$t('layout.offDashboard')}</span>
				</span>
				<button
					type="button"
					class="flex h-11 flex-none items-center rounded-control bg-accent-soft px-3.5 text-button text-accent"
					aria-label={$t('layout.showCard', { name: nameOf(key) })}
					onclick={() => show(key)}
				>
					{$t('common.show')}
				</button>
			</div>
		{:else}
			<p class="px-0.5 text-body text-muted">{$t('layout.noHiddenCards')}</p>
		{/each}
	</div>

	<div
		class="fixed inset-x-0 bottom-0 z-40 flex gap-2.5 border-t border-border bg-card-solid px-4 pt-3 pb-[calc(12px+env(safe-area-inset-bottom))] lg:hidden"
	>
		<button
			type="button"
			class="flex h-12 flex-1 items-center justify-center rounded-[11px] border border-border text-[14px] leading-none font-semibold"
			onclick={oncancel}
		>
			{$t('common.cancel')}
		</button>
		<button
			type="button"
			class="flex h-12 flex-[1.4] items-center justify-center rounded-[11px] bg-btn-primary text-[14px] leading-none font-semibold text-btn-primary-text"
			onclick={save}
		>
			{$t('layout.saveLayout')}
		</button>
	</div>
{:else}
	<div class="grid grid-cols-1 gap-3.5 lg:grid-cols-6 lg:gap-5">
		{#each visibleKeys as key (key)}
			<div class="min-w-0 lg:col-span-(--span)" style:--span={spanOf(key)}>
				{@render card(key, 'full')}
			</div>
		{/each}
	</div>
{/if}
