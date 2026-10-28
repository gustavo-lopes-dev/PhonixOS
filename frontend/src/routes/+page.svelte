<script lang="ts">
	// Página raiz do painel (handoff §A/§B/§C; telas 1a–6c, exceto Terminal). Compõe a casca
	// (3.3b), a grade com métricas e lançador (3.3) e as configurações (3.5). Abre o WebSocket
	// enquanto montada e carrega configurações, layout e atalhos em paralelo, cada um com estado
	// próprio de carregamento/erro. Tema e Lite já são aplicados por `+layout.svelte`/`settings.ts`.
	import { onMount } from 'svelte';
	import AppLauncher from '#lib/components/AppLauncher.svelte';
	import AppSidebar from '#lib/components/AppSidebar.svelte';
	import ConnectionBanner from '#lib/components/ConnectionBanner.svelte';
	import DashboardHeader from '#lib/components/DashboardHeader.svelte';
	import Grid from '#lib/components/Grid.svelte';
	import MetricCard from '#lib/components/MetricCard.svelte';
	import NetworkLine from '#lib/components/NetworkLine.svelte';
	import SettingsPanel from '#lib/components/SettingsPanel.svelte';
	import ShortcutDialog from '#lib/components/ShortcutDialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.ts';
	import { CARD_KEYS, type CardKey } from '#lib/layout.ts';
	import { api, ApiError, type LocalErrorCode } from '#lib/services/api.ts';
	import { telemetrySocket } from '#lib/services/websocket.ts';
	import { updateIntervalSeconds } from '#lib/stores/connection.ts';
	import { dismissInstall, installAvailable, promptInstall } from '#lib/stores/install.ts';
	import { effectivePollIntervalS, loadSettings, settingsStore } from '#lib/stores/settings.ts';
	import type {
		CreateShortcutDTO,
		LayoutCard,
		Shortcut,
		UpdateLayoutCardDTO,
		UpdateShortcutDTO
	} from '../types/dashboard.ts';

	type LoadStatus = 'loading' | 'ready' | 'error';
	type Resource = 'settings' | 'layout' | 'shortcuts';
	type ActionError = 'layoutSave' | 'shortcutSave' | 'shortcutDelete';

	const loadErrorKey: Record<Resource, MessageKey> = {
		settings: 'dashboard.settingsLoadError',
		layout: 'dashboard.layoutLoadError',
		shortcuts: 'dashboard.shortcutsLoadError'
	};
	const actionErrorKey: Record<ActionError, MessageKey> = {
		layoutSave: 'dashboard.layoutSaveError',
		shortcutSave: 'dashboard.shortcutSaveError',
		shortcutDelete: 'dashboard.shortcutDeleteError'
	};
	const localErrorKey: Record<LocalErrorCode, MessageKey> = {
		network_error: 'common.errorNetwork',
		timeout: 'common.errorTimeout',
		invalid_response: 'common.errorInvalidResponse'
	};
	const metricKind: Record<Exclude<CardKey, 'app_launcher'>, 'cpu' | 'memory' | 'battery'> = {
		metrics_cpu: 'cpu',
		metrics_memory: 'memory',
		metrics_battery: 'battery'
	};

	// Disposição do catálogo (seed + migration 002) exibida, sem edição, até o layout chegar:
	// mantém os skeletons de `4a` em vez de uma página vazia.
	const PLACEHOLDER_LAYOUT: LayoutCard[] = CARD_KEYS.map((card_key, index) => {
		const launcher = card_key === 'app_launcher';
		return {
			id: index + 1,
			card_key,
			pos_x: launcher ? 0 : index * 2,
			pos_y: launcher ? 2 : 0,
			width: launcher ? 6 : 2,
			height: launcher ? 4 : 2,
			min_width: 2,
			min_height: 2,
			is_visible: true,
			updated_at: ''
		};
	});

	let status = $state<Record<Resource, LoadStatus>>({ settings: 'loading', layout: 'loading', shortcuts: 'loading' });
	let loadErrors = $state<Partial<Record<Resource, unknown>>>({});
	let layout = $state<LayoutCard[] | null>(null);
	let shortcuts = $state<Shortcut[]>([]);

	let editingLayout = $state(false);
	let savingLayout = $state(false);
	let editingShortcuts = $state(false);
	let actionError = $state<{ kind: ActionError; error: unknown } | null>(null);
	let settingsOpen = $state(false);
	let pinDialogOpen = $state(false);

	const instanceName = $derived($settingsStore?.instance_name ?? $t('shell.brandName'));
	const pollIntervalS = $derived($effectivePollIntervalS ?? $updateIntervalSeconds ?? 0);
	const pinned = $derived(shortcuts.filter((shortcut) => shortcut.is_pinned));
	const categories = $derived([
		...new Set(shortcuts.map((shortcut) => shortcut.category.trim()).filter((name) => name !== ''))
	]);
	const nextDisplayOrder = $derived(
		shortcuts.reduce((max, shortcut) => Math.max(max, shortcut.display_order + 1), 0)
	);
	const failedLoads = $derived((['settings', 'layout', 'shortcuts'] as const).filter((key) => status[key] === 'error'));
	/** Uma faixa para todas as falhas de carga: título específico se só uma falhou. */
	const loadNoticeTitle = $derived(
		failedLoads.length === 1 && failedLoads[0] ? $t(loadErrorKey[failedLoads[0]]) : $t('dashboard.loadErrorMany')
	);

	function errorText(error: unknown): string {
		if (error instanceof ApiError && !error.isLocal) return error.message;
		const code = error instanceof ApiError && error.isLocal ? (error.code as LocalErrorCode) : 'invalid_response';
		return $t(localErrorKey[code]);
	}

	async function load<T>(resource: Resource, request: () => Promise<T>, apply: (value: T) => void) {
		status[resource] = 'loading';
		delete loadErrors[resource];
		try {
			apply(await request());
			status[resource] = 'ready';
		} catch (error) {
			loadErrors[resource] = error;
			status[resource] = 'error';
		}
	}

	const loaders: Record<Resource, () => Promise<void>> = {
		settings: () => load('settings', loadSettings, () => {}),
		layout: () => load('layout', api.getLayout, (cards) => (layout = cards)),
		shortcuts: () => load('shortcuts', api.listShortcuts, (list) => (shortcuts = list))
	};

	function retryFailedLoads() {
		for (const resource of failedLoads) loaders[resource]();
	}

	/** Recarrega a lista após uma escrita (ordem do servidor: `display_order`, `id`). */
	async function reloadShortcuts() {
		try {
			shortcuts = await api.listShortcuts();
			status.shortcuts = 'ready';
			delete loadErrors.shortcuts;
		} catch (error) {
			loadErrors.shortcuts = error;
			status.shortcuts = 'error';
		}
	}

	onMount(() => {
		telemetrySocket.connect();
		for (const loader of Object.values(loaders)) loader();
		return () => telemetrySocket.disconnect();
	});

	function toggleLayoutEditing() {
		if (savingLayout || layout === null) return;
		if (editingLayout) cancelLayout();
		else {
			actionError = null;
			editingLayout = true;
		}
	}

	async function saveLayout(cards: UpdateLayoutCardDTO[]) {
		if (savingLayout) return;
		savingLayout = true;
		actionError = null;
		try {
			layout = await api.updateLayout(cards);
			editingLayout = false;
		} catch (error) {
			// Permanece em edição, com o rascunho preservado e a mensagem do envelope.
			actionError = { kind: 'layoutSave', error };
		} finally {
			savingLayout = false;
		}
	}

	function cancelLayout() {
		if (savingLayout) return;
		if (actionError?.kind === 'layoutSave') actionError = null;
		editingLayout = false;
	}

	async function runShortcutAction(kind: ActionError, action: () => Promise<unknown>) {
		actionError = null;
		try {
			await action();
		} catch (error) {
			actionError = { kind, error };
			return;
		}
		await reloadShortcuts();
	}

	const createShortcut = (dto: CreateShortcutDTO) => runShortcutAction('shortcutSave', () => api.createShortcut(dto));
	const updateShortcut = (id: number, dto: UpdateShortcutDTO) =>
		runShortcutAction('shortcutSave', () => api.updateShortcut(id, dto));
	const deleteShortcut = (id: number) => runShortcutAction('shortcutDelete', () => api.deleteShortcut(id));

	function pinService(dto: CreateShortcutDTO) {
		pinDialogOpen = false;
		createShortcut({ ...dto, display_order: nextDisplayOrder });
	}
</script>

{#snippet notice(title: string, message: string, action: string, onaction: () => void)}
	<div role="alert" class="flex items-center gap-3 rounded-[14px] border border-warn-border bg-warn-soft py-3 pr-3 pl-4 lg:gap-4 lg:pl-5">
		<span
			aria-hidden="true"
			class="grid size-6 flex-none place-items-center rounded-full border-2 border-warn text-[13px] leading-none font-bold text-warn"
		>
			!
		</span>
		<div class="flex min-w-0 flex-1 flex-col gap-1">
			<span class="text-[14px] leading-[1.2] font-semibold">{title}</span>
			<span class="text-body leading-[1.4] text-muted">{message}</span>
		</div>
		<button
			type="button"
			class="flex h-11 flex-none items-center rounded-control border border-border bg-inner px-3.5 text-button lg:h-9 lg:rounded-[9px]"
			onclick={onaction}
		>
			{action}
		</button>
	</div>
{/snippet}

{#snippet card(key: CardKey, mode: 'full' | 'summary')}
	{#if key === 'app_launcher'}
		<AppLauncher
			{shortcuts}
			loading={status.shortcuts !== 'ready'}
			bind:editing={editingShortcuts}
			summary={mode === 'summary'}
			oncreate={createShortcut}
			onupdate={updateShortcut}
			ondelete={deleteShortcut}
		/>
	{:else}
		<MetricCard kind={metricKind[key]} summary={mode === 'summary'} />
	{/if}
{/snippet}

<div class="flex min-h-dvh">
	<AppSidebar
		{pinned}
		oneditshortcuts={() => (editingShortcuts = !editingShortcuts)}
		onpinservice={() => (pinDialogOpen = true)}
		onopensettings={() => (settingsOpen = true)}
	/>

	<main
		class="flex min-w-0 flex-1 flex-col gap-3.5 pt-[calc(14px+env(safe-area-inset-top))] pr-[max(16px,env(safe-area-inset-right))] pb-[calc(28px+env(safe-area-inset-bottom))] pl-[max(16px,env(safe-area-inset-left))] lg:gap-6 lg:pt-8 lg:pr-10 lg:pb-10 lg:pl-10"
	>
		<DashboardHeader
			{instanceName}
			{pollIntervalS}
			editing={editingLayout}
			installAvailable={$installAvailable}
			oneditlayout={toggleLayoutEditing}
			onopensettings={() => (settingsOpen = true)}
			oninstall={promptInstall}
			ondismissinstall={dismissInstall}
		/>

		<ConnectionBanner />

		{#if failedLoads[0]}
			{@render notice(loadNoticeTitle, errorText(loadErrors[failedLoads[0]]), $t('common.retry'), retryFailedLoads)}
		{/if}

		{#if actionError}
			{@render notice($t(actionErrorKey[actionError.kind]), errorText(actionError.error), $t('common.close'), () => (actionError = null))}
		{/if}

		{#if savingLayout}
			<p class="text-body text-muted" role="status">{$t('dashboard.savingLayout')}</p>
		{/if}

		<!-- `disabled` no fieldset desabilita os botões da edição durante o salvamento. -->
		<fieldset class="contents" disabled={savingLayout} aria-busy={savingLayout}>
			<Grid
				cards={layout ?? PLACEHOLDER_LAYOUT}
				editing={editingLayout}
				{card}
				onsave={saveLayout}
				oncancel={cancelLayout}
			/>
		</fieldset>

		{#if !editingLayout}
			<footer class="pt-1.5 lg:hidden">
				<NetworkLine variant="footer" />
			</footer>
		{/if}
	</main>
</div>

<SettingsPanel open={settingsOpen} onclose={() => (settingsOpen = false)} />

{#if pinDialogOpen}
	<ShortcutDialog
		shortcut={null}
		{categories}
		initialPinned
		oncreate={pinService}
		onupdate={updateShortcut}
		ondelete={deleteShortcut}
		onclose={() => (pinDialogOpen = false)}
	/>
{/if}
