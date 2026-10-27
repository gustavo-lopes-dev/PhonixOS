<script lang="ts">
	// Cabeçalho do painel (handoff §A "Cabeçalho", §B; telas 1a/1b/4a/4b/4c/4d).
	// Desktop (≥ 1024 px): H1 + pílula/detalhe + linha de rede à esquerda; à direita a chamada de
	// instalação compacta e o lápis "Editar layout" com `margin-top: auto` (alinha com o card de
	// Bateria). Mobile: marca + H1 22 px + "Editar"/"Ajustes" (44 px), pílula/detalhe e o cartão de
	// instalação; a linha de rede vai para o rodapé da página (`NetworkLine variant="footer"`).
	// Na Fase 3 não há abas "Painel | Terminal" nem seletor de ambiente (handoff §Lacunas 4).
	import InstallPrompt from '#lib/components/InstallPrompt.svelte';
	import NetworkLine from '#lib/components/NetworkLine.svelte';
	import StatusPill from '#lib/components/StatusPill.svelte';
	import { t } from '#lib/i18n/index.ts';
	import { formatters } from '#lib/i18n/format.ts';
	import { connectionStore, lastUpdateAgeSeconds } from '#lib/stores/connection.ts';
	import { metricsStore } from '#lib/stores/metrics.ts';

	interface Props {
		/** `DashboardSettings.instance_name` (contracts.md §4.2). */
		instanceName: string;
		/** Cadência efetiva em segundos, calculada pela página. */
		pollIntervalS: number;
		/** Modo de edição do layout ativo. */
		editing: boolean;
		/** `beforeinstallprompt` ocorreu e o prompt ainda está disponível. */
		installAvailable: boolean;
		oneditlayout: () => void;
		onopensettings: () => void;
		oninstall: () => void;
		ondismissinstall: () => void;
	}

	let {
		instanceName,
		pollIntervalS,
		editing,
		installAvailable,
		oneditlayout,
		onopensettings,
		oninstall,
		ondismissinstall
	}: Props = $props();

	const status = $derived($connectionStore.state);
	const lastReadingAt = $derived($connectionStore.lastUpdateAt ?? $metricsStore?.timestamp ?? null);
	const time = $derived(lastReadingAt === null ? '' : $formatters.time(lastReadingAt));
	const age = $derived($lastUpdateAgeSeconds ?? 0);

	const detail = $derived.by(() => {
		if (status === 'live') return $t('connection.updatedEvery', { time, n: pollIntervalS });
		if (status === 'stale') return $t('shell.staleDetail', { time, n: age });
		return '';
	});
	const detailShort = $derived.by(() => {
		if (status === 'live') return $t('shell.updatedShort', { time, n: pollIntervalS });
		if (status === 'stale') return $t('shell.staleDetailShort', { time, n: age });
		return '';
	});
</script>

<header>
	<!-- Desktop -->
	<div class="hidden items-stretch justify-between gap-6 lg:flex">
		<div class="flex min-w-0 flex-col gap-3">
			<h1 class="text-page-title">{instanceName}</h1>
			<div class="flex flex-wrap items-center gap-3.5">
				<StatusPill />
				{#if detail}
					<span class="text-[13px] leading-[1.2] text-muted">{detail}</span>
				{/if}
			</div>
			<NetworkLine variant="header" />
		</div>
		<div class="flex flex-none flex-col items-end gap-3">
			{#if installAvailable}
				<InstallPrompt variant="compact" {oninstall} />
			{/if}
			<button
				type="button"
				aria-label={$t('shell.editLayout')}
				aria-pressed={editing}
				class={[
					'mt-auto grid size-10 place-items-center rounded-control border text-[19px] leading-none',
					editing ? 'border-accent bg-accent-soft text-accent' : 'border-border bg-card text-text'
				]}
				onclick={oneditlayout}
			>
				<span aria-hidden="true">✎</span>
			</button>
		</div>
	</div>

	<!-- Mobile -->
	<div class="flex flex-col gap-3 lg:hidden">
		<div class="flex items-center gap-2.5">
			<span
				aria-hidden="true"
				class="grid size-9 flex-none place-items-center rounded-control bg-btn-primary text-[16px] leading-none font-bold text-btn-primary-text"
			>
				{$t('shell.brandMark')}
			</span>
			<h1 class="min-w-0 flex-1 truncate text-page-title-mobile">
				{editing ? $t('shell.editLayout') : instanceName}
			</h1>
			{#if !editing}
				<button
					type="button"
					aria-label={$t('shell.editLayout')}
					class="flex h-11 flex-none items-center rounded-control border border-border bg-card px-3 text-button font-semibold text-text"
					onclick={oneditlayout}
				>
					{$t('common.edit')}
				</button>
				<button
					type="button"
					class="flex h-11 flex-none items-center rounded-control border border-border bg-card px-3 text-button font-semibold text-text"
					onclick={onopensettings}
				>
					{$t('shell.settingsShort')}
				</button>
			{/if}
		</div>
		{#if !editing}
			<div class="flex flex-wrap items-center gap-2.5">
				<StatusPill />
				{#if detailShort}
					<span class="text-label leading-[1.3] text-muted">{detailShort}</span>
				{/if}
			</div>
			{#if installAvailable}
				<InstallPrompt variant="card" {oninstall} ondismiss={ondismissinstall} />
			{/if}
		{/if}
	</div>
</header>
