<script lang="ts">
	// Sidebar desktop (handoff §A "Sidebar"): 240 px, altura da viewport, não rola — só a lista de
	// acesso rápido rola. Oculta abaixo de 1024 px. Na Fase 3 só existe a página "Painel": o item
	// "Terminal" depende de backend inexistente (handoff §Lacunas 4) e não é renderizado.
	// Apresentacional: recebe os atalhos fixados e emite callbacks; a página (3.7) conecta tudo.
	import Favicon from '#lib/components/Favicon.svelte';
	import { t } from '#lib/i18n/index.ts';
	import type { Shortcut } from '../../types/dashboard.ts';

	interface Props {
		/** Atalhos com `is_pinned === true` (contracts.md §4.2); outros são ignorados. */
		pinned: readonly Shortcut[];
		/** "Editar" do acesso rápido (a página alterna a edição do lançador). */
		oneditshortcuts: () => void;
		/** "+ Fixar serviço". */
		onpinservice: () => void;
		onopensettings: () => void;
	}

	let { pinned, oneditshortcuts, onpinservice, onopensettings }: Props = $props();

	const quickAccessId = $props.id();

	const items = $derived(
		pinned
			.filter((shortcut) => shortcut.is_pinned === true)
			.sort((a, b) => a.display_order - b.display_order || a.id - b.id)
	);
</script>

<aside
	class="sticky top-0 hidden h-dvh w-60 flex-none flex-col gap-6 self-start border-r border-border bg-rail px-4 pt-7 pb-5 [backdrop-filter:var(--surface-blur)] lg:flex"
>
	<!-- Marca -->
	<div class="flex items-center gap-[11px] px-2">
		<span
			aria-hidden="true"
			class="grid size-9 flex-none place-items-center rounded-control bg-btn-primary text-[16px] leading-none font-bold text-btn-primary-text"
		>
			{$t('shell.brandMark')}
		</span>
		<div class="flex flex-col gap-[3px]">
			<span class="text-[15px] leading-none font-semibold">{$t('shell.brandName')}</span>
			<span class="num text-[11px] leading-none text-muted">{$t('shell.version', { version: __APP_VERSION__ })}</span>
		</div>
	</div>

	<!-- Página -->
	<nav aria-label={$t('shell.page')} class="flex flex-col gap-1">
		<span aria-hidden="true" class="px-2.5 pb-2 text-rail-label text-muted uppercase">{$t('shell.page')}</span>
		<a
			href="/"
			aria-current="page"
			class="flex h-10 items-center gap-2.5 rounded-control bg-accent-soft px-2.5 text-nav text-accent no-underline"
		>
			<span aria-hidden="true" class="grid grid-cols-[repeat(2,6px)] gap-0.5">
				<i class="size-1.5 rounded-[1.5px] bg-current"></i>
				<i class="size-1.5 rounded-[1.5px] bg-current"></i>
				<i class="size-1.5 rounded-[1.5px] bg-current"></i>
				<i class="size-1.5 rounded-[1.5px] bg-current"></i>
			</span>
			{$t('shell.dashboard')}
		</a>
	</nav>

	<!-- Acesso rápido -->
	<section aria-labelledby={quickAccessId} class="flex min-h-0 flex-1 flex-col gap-1">
		<div class="flex items-center justify-between pr-1 pb-2 pl-2.5">
			<h2 id={quickAccessId} class="text-rail-label text-muted uppercase">{$t('shell.quickAccess')}</h2>
			<button
				type="button"
				aria-label={$t('shell.editQuickAccess')}
				class="flex h-[26px] items-center rounded-badge px-2 text-meta leading-none font-semibold text-accent"
				onclick={oneditshortcuts}
			>
				{$t('common.edit')}
			</button>
		</div>
		<div class="relative min-h-0 flex-1">
			<div class="flex h-full flex-col gap-0.5 overflow-y-auto pr-1 pb-7 [scrollbar-width:thin]">
				{#if items.length === 0}
					<p class="px-2.5 py-2 text-label text-muted">{$t('shell.quickAccessEmpty')}</p>
				{:else}
					<ul class="flex flex-col gap-0.5">
						{#each items as shortcut (shortcut.id)}
							<li>
								<a
									href={shortcut.url}
									target="_blank"
									rel="noopener noreferrer"
									title={shortcut.url}
									class="flex min-h-10 items-center gap-2.5 rounded-control px-2.5 text-text no-underline hover:bg-inner"
								>
									<Favicon src={shortcut.icon_url} title={shortcut.title} size={22} radius={6} />
									<span class="min-w-0 flex-1 truncate text-[13.5px] leading-[1.2] font-medium">{shortcut.title}</span>
									<span aria-hidden="true" class="text-meta leading-none text-muted">↗</span>
								</a>
							</li>
						{/each}
					</ul>
				{/if}
				<button
					type="button"
					class="mt-1 flex min-h-10 flex-none items-center gap-2.5 rounded-control border border-dashed border-border px-2.5 text-[13px] leading-none font-medium text-muted"
					onclick={onpinservice}
				>
					<span aria-hidden="true" class="w-[22px] text-center text-[16px] leading-none">+</span>
					{$t('shell.pinService')}
				</button>
			</div>
			<div aria-hidden="true" class="pointer-events-none absolute inset-x-0 bottom-0 h-7 bg-linear-to-b from-transparent to-rail-fade"></div>
		</div>
	</section>

	<!-- Rodapé -->
	<div class="flex flex-none flex-col gap-1 border-t border-inner-border pt-3">
		<button
			type="button"
			class="flex h-10 items-center gap-2.5 rounded-control px-2.5 text-nav text-text hover:bg-inner"
			onclick={onopensettings}
		>
			<span aria-hidden="true" class="grid size-4 place-items-center text-[19px] leading-none font-normal">⚙</span>
			{$t('shell.settings')}
		</button>
	</div>
</aside>
