<script lang="ts">
	// Lançador de serviços (handoff §A item 4, §B): atalhos agrupados por categoria na ordem de
	// primeira aparição após `display_order`. Tiles abrem o serviço em nova aba; no modo "Editar
	// atalhos" abrem o `ShortcutDialog`. Não chama a API: emite os DTOs do contrato.
	// `summary` = linha compacta de tiles usada no modo de edição do layout.
	import { tick } from 'svelte';
	import Card from '#lib/components/Card.svelte';
	import Favicon from '#lib/components/Favicon.svelte';
	import ShortcutDialog from '#lib/components/ShortcutDialog.svelte';
	import { t } from '#lib/i18n/index.ts';
	import type { CreateShortcutDTO, Shortcut, UpdateShortcutDTO } from '../../types/dashboard.ts';

	interface Props {
		shortcuts: readonly Shortcut[];
		loading?: boolean;
		/** A carga da lista falhou (a página mostra a faixa com "Tentar novamente"). */
		failed?: boolean;
		/** Modo "Editar atalhos"; também alternável de fora (sidebar, 3.7). */
		editing?: boolean;
		summary?: boolean;
		oncreate: (dto: CreateShortcutDTO) => void;
		onupdate: (id: number, dto: UpdateShortcutDTO) => void;
		/** Pode devolver a Promise da exclusão: o foco só é reposicionado depois dela. */
		ondelete: (id: number) => void | Promise<unknown>;
	}

	let {
		shortcuts,
		loading = false,
		failed = false,
		editing = $bindable(false),
		summary = false,
		oncreate,
		onupdate,
		ondelete
	}: Props = $props();

	interface Group {
		name: string;
		items: Shortcut[];
	}

	/** `null` = fechado; `{ shortcut: null }` = novo atalho. */
	let dialog = $state<{ shortcut: Shortcut | null } | null>(null);
	let tileList = $state<HTMLElement>();
	let editToggle = $state<HTMLButtonElement>();

	const sorted = $derived([...shortcuts].sort((a, b) => a.display_order - b.display_order || a.id - b.id));
	const groups = $derived.by(() => {
		const byName = new Map<string, Group>();
		for (const shortcut of sorted) {
			const name = shortcut.category.trim();
			let group = byName.get(name);
			if (!group) {
				group = { name, items: [] };
				byName.set(name, group);
			}
			group.items.push(shortcut);
		}
		return [...byName.values()];
	});
	const categoryNames = $derived(groups.map((group) => group.name).filter((name) => name !== ''));
	const nextDisplayOrder = $derived(sorted.reduce((max, shortcut) => Math.max(max, shortcut.display_order + 1), 0));

	/** Host (e caminho) exibido sob o título: `192.168.1.2/admin`. */
	function hostOf(url: string): string {
		try {
			const parsed = new URL(url);
			const path = parsed.pathname === '/' ? '' : parsed.pathname.replace(/\/$/, '');
			return parsed.host + path;
		} catch {
			return url;
		}
	}

	function create(dto: CreateShortcutDTO) {
		oncreate({ ...dto, display_order: nextDisplayOrder });
		dialog = null;
	}

	function update(id: number, dto: UpdateShortcutDTO) {
		onupdate(id, dto);
		dialog = null;
	}

	/** Tiles na ordem em que aparecem na tela (agrupados por categoria). */
	function tiles(): HTMLElement[] {
		return tileList ? Array.from(tileList.querySelectorAll<HTMLElement>('[data-shortcut-tile]')) : [];
	}

	// O gatilho do diálogo (o tile excluído) sai do DOM: o foco vai para o tile que ocupou a
	// mesma posição na tela, o anterior se era o último, ou o botão "Concluir edição".
	async function remove(id: number) {
		const index = tiles().findIndex((tile) => tile.dataset.shortcutTile === String(id));
		dialog = null;
		await ondelete(id);
		await tick();
		const remaining = tiles();
		const target = index < 0 ? undefined : remaining[Math.min(index, remaining.length - 1)];
		(target ?? editToggle)?.focus();
	}
</script>

{#snippet tileContent(shortcut: Shortcut, mark: string)}
	<Favicon src={shortcut.icon_url} title={shortcut.title} size={34} radius={9} mobileSize={30} mobileRadius={8} />
	<span class="flex min-w-0 flex-1 flex-col gap-[5px]">
		<!-- Celular: no máximo 2 linhas para manter o tile de 56 px (handoff §B); desktop: 1 linha. -->
		<span
			class="line-clamp-2 text-body leading-[1.2] font-semibold [overflow-wrap:anywhere] lg:line-clamp-none lg:truncate lg:text-[14px] lg:leading-[1.1]"
			>{shortcut.title}</span
		>
		<span class="num truncate text-[11.5px] leading-none text-muted max-lg:hidden">{hostOf(shortcut.url)}</span>
	</span>
	<span class={['text-[14px] leading-none', editing ? 'text-accent' : 'text-muted max-lg:hidden']} aria-hidden="true">
		{mark}
	</span>
{/snippet}

{#if summary}
	<div class="flex flex-col gap-3">
		<span class="text-card-title uppercase">{$t('launcher.title')}</span>
		{#if loading}
			<span class="text-body text-muted">{$t('launcher.loading')}</span>
		{:else if failed}
			<span class="text-body text-muted">{$t('launcher.loadError')}</span>
		{:else if sorted.length === 0}
			<span class="text-body text-muted">{$t('launcher.empty')}</span>
		{:else}
			<ul class="grid grid-cols-[repeat(auto-fill,minmax(112px,1fr))] gap-2.5 opacity-55">
				{#each sorted as shortcut (shortcut.id)}
					<li class="surface-inner flex h-11 min-w-0 items-center gap-2 rounded-control px-2.5">
						<Favicon src={shortcut.icon_url} title={shortcut.title} size={22} radius={6} />
						<span class="truncate text-meta leading-[1.1] font-medium">{shortcut.title}</span>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
{:else}
	<Card title={$t('launcher.title')} fixedHeight={false}>
		{#snippet header(titleId)}
			<div class="flex flex-col gap-3.5 lg:flex-row lg:items-center lg:justify-between lg:gap-4">
				<div class="flex items-baseline justify-between gap-3 lg:justify-start">
					<h2 id={titleId} class="text-card-title uppercase">{$t('launcher.title')}</h2>
					{#if !loading && !failed && sorted.length > 0}
						<span class="text-meta leading-none text-muted lg:text-label lg:leading-none">
							{$t('launcher.shortcutCount', { count: sorted.length })}<span class="max-lg:hidden"
								>{' · '}{$t('launcher.categoryCount', { count: groups.length })}</span
							>
						</span>
					{/if}
				</div>
				{#if loading}
					<span class="text-label leading-none text-muted" role="status">{$t('launcher.loading')}</span>
				{:else if !failed}
					<div class="grid grid-cols-[1fr_1.5fr] gap-2 lg:flex lg:gap-2.5">
						<button
							bind:this={editToggle}
							type="button"
							aria-pressed={editing}
							class={[
								'flex h-11 items-center justify-center rounded-control border px-3.5 text-button lg:h-9 lg:rounded-[9px]',
								editing ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-inner'
							]}
							onclick={() => (editing = !editing)}
						>
							{$t(editing ? 'launcher.doneEditing' : 'launcher.editShortcuts')}
						</button>
						<button
							type="button"
							class="flex h-11 items-center justify-center rounded-control bg-btn-primary px-3.5 text-button text-btn-primary-text lg:h-9 lg:rounded-[9px]"
							onclick={() => (dialog = { shortcut: null })}
						>
							{$t('launcher.addShortcut')}
						</button>
					</div>
				{/if}
			</div>
		{/snippet}

		{#if editing && !loading && !failed && sorted.length > 0}
			<p class="text-label text-muted">{$t('launcher.editingHint')}</p>
		{/if}

		{#if loading}
			<div class="grid grid-cols-2 gap-2 lg:grid-cols-4 lg:gap-3" aria-hidden="true">
				{#each { length: 8 }, index (index)}
					<div class="surface-inner h-14 rounded-tile lg:h-[62px]"></div>
				{/each}
			</div>
		{:else if failed}
			<div class="flex flex-col gap-1.5 rounded-tile border border-dashed border-warn-border px-4 py-5" role="status">
				<span class="text-body font-semibold">{$t('launcher.loadError')}</span>
			</div>
		{:else if sorted.length === 0}
			<div class="flex flex-col gap-1.5 rounded-tile border border-dashed border-border px-4 py-5">
				<span class="text-body font-semibold">{$t('launcher.empty')}</span>
				<span class="text-body text-muted">{$t('launcher.emptyHint')}</span>
			</div>
		{:else}
			<div
				bind:this={tileList}
				class="flex flex-col gap-2 lg:grid lg:gap-5"
				style:grid-template-columns="repeat({Math.min(groups.length, 4)}, minmax(0, 1fr))"
			>
				{#each groups as group (group.name)}
					<section class="flex min-w-0 flex-col gap-2 lg:gap-2.5">
						<h3
							class="pt-1 text-meta leading-none font-medium text-muted lg:border-b lg:border-inner-border lg:pt-0 lg:pb-2 lg:text-label lg:leading-none"
						>
							{group.name === '' ? $t('launcher.defaultCategory') : group.name}
						</h3>
						<ul class="grid grid-cols-2 gap-2 lg:flex lg:flex-col lg:gap-2.5">
							{#each group.items as shortcut (shortcut.id)}
								<li class="min-w-0">
									{#if editing}
										<button
											type="button"
											data-shortcut-tile={shortcut.id}
											class="flex min-h-14 w-full items-center gap-2.5 rounded-tile border-[1.5px] border-dashed border-accent bg-inner px-2.5 text-left lg:h-[62px] lg:gap-3 lg:px-3.5"
											aria-label={$t('launcher.editShortcutLabel', { title: shortcut.title })}
											onclick={() => (dialog = { shortcut })}
										>
											{@render tileContent(shortcut, '✎')}
										</button>
									{:else}
										<a
											data-shortcut-tile={shortcut.id}
											href={shortcut.url}
											target="_blank"
											rel="noopener noreferrer"
											title={shortcut.url}
											class="surface-inner flex min-h-14 w-full items-center gap-2.5 rounded-tile px-2.5 text-text no-underline lg:h-[62px] lg:gap-3 lg:px-3.5"
										>
											{@render tileContent(shortcut, '↗')}
										</a>
									{/if}
								</li>
							{/each}
						</ul>
					</section>
				{/each}
			</div>
		{/if}
	</Card>
{/if}

{#if dialog}
	<ShortcutDialog
		shortcut={dialog.shortcut}
		categories={categoryNames}
		oncreate={create}
		onupdate={update}
		ondelete={remove}
		onclose={() => (dialog = null)}
	/>
{/if}
