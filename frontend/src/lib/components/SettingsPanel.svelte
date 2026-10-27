<script lang="ts">
	// Diálogo de configurações (handoff §D, `3a`/`6c`) sobre `Dialog`. Preferências do servidor
	// ficam num rascunho local até "Salvar alterações" (PATCH só com os campos alterados);
	// "Descartar" e fechar restauram os valores do servidor. O idioma é preferência local
	// (`setLocale`), aplicada na hora e fora do rascunho. `battery_saver_threshold` não é exibido.
	import { untrack } from 'svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Dropdown, { type DropdownOption } from '#lib/components/Dropdown.svelte';
	import { formatters } from '#lib/i18n/format.ts';
	import { availableLocales, setLocale, t, type MessageKey } from '#lib/i18n/index.ts';
	import { ApiError, type LocalErrorCode } from '#lib/services/api.ts';
	import { localeStore } from '#lib/stores/locale.ts';
	import { serverProfileStore } from '#lib/stores/profile.ts';
	import {
		loadSettings,
		saveSettings,
		settingsChanges,
		settingsStore,
		type ThemeMode
	} from '#lib/stores/settings.ts';
	import type { DashboardSettings, UpdateSettingsDTO } from '../../types/dashboard.ts';
	import type { ProfileType } from '../../types/telemetry.ts';

	interface Props {
		open: boolean;
		onclose: () => void;
	}

	let { open, onclose }: Props = $props();

	type IntervalMode = 'auto' | 'custom';

	interface Draft {
		name: string;
		theme: ThemeMode;
		lite: boolean;
		interval: IntervalMode;
		/** Valor do slider (1–60 s), mantido mesmo no modo automático. */
		seconds: number;
	}

	// Limites do contrato (contracts.md §3.4: instance_name 1..32; poll 0 ou 1000..60000 ms).
	const NAME_MAX = 32;
	const MIN_SECONDS = 1;
	const MAX_SECONDS = 60;
	const DEFAULT_CUSTOM_SECONDS = 5;
	const THEME_MODES: readonly ThemeMode[] = ['dark', 'light', 'system'];

	const themeKey: Record<ThemeMode, MessageKey> = {
		dark: 'settings.themeDark',
		light: 'settings.themeLight',
		system: 'settings.themeSystem'
	};
	const profileKey: Record<ProfileType, MessageKey> = {
		lite: 'settings.profileLite',
		performance: 'settings.profilePerformance'
	};
	const localErrorKey: Record<LocalErrorCode, MessageKey> = {
		network_error: 'common.errorNetwork',
		timeout: 'common.errorTimeout',
		invalid_response: 'common.errorInvalidResponse'
	};

	const ids = $props.id();

	let draft = $state<Draft | null>(null);
	let loading = $state(false);
	let loadError = $state<unknown>(null);
	let saving = $state(false);
	let saveError = $state<unknown>(null);

	const settings = $derived($settingsStore);

	function secondsOf(ms: number): number {
		return Math.min(MAX_SECONDS, Math.max(MIN_SECONDS, Math.round(ms / 1000)));
	}

	function toDraft(source: DashboardSettings): Draft {
		const custom = source.custom_poll_interval_ms > 0;
		return {
			name: source.instance_name,
			theme: source.theme_mode,
			lite: source.force_lite_mode,
			interval: custom ? 'custom' : 'auto',
			seconds: custom ? secondsOf(source.custom_poll_interval_ms) : DEFAULT_CUSTOM_SECONDS
		};
	}

	/** Cadência do rascunho; preserva um valor do servidor fora de segundos inteiros se o slider não mudou. */
	function draftIntervalMs(current: Draft, source: DashboardSettings): number {
		if (current.interval === 'auto') return 0;
		const serverMs = source.custom_poll_interval_ms;
		return serverMs > 0 && current.seconds === secondsOf(serverMs) ? serverMs : current.seconds * 1000;
	}

	/** Somente os campos alterados (`settingsChanges` nunca inclui `battery_saver_threshold`). */
	const changes = $derived.by((): UpdateSettingsDTO => {
		if (!draft || !settings) return {};
		return settingsChanges(settings, {
			instance_name: draft.name.trim(),
			theme_mode: draft.theme,
			force_lite_mode: draft.lite,
			custom_poll_interval_ms: draftIntervalMs(draft, settings)
		});
	});
	const dirty = $derived(Object.keys(changes).length > 0);
	const nameInvalid = $derived(draft !== null && draft.name.trim() === '');
	const canSave = $derived(dirty && !nameInvalid && !saving);

	async function load() {
		loading = true;
		loadError = null;
		try {
			await loadSettings();
		} catch (error) {
			loadError = error;
		} finally {
			loading = false;
		}
	}

	// Rascunho criado a cada abertura (e quando a primeira carga chega); fechar descarta tudo.
	$effect(() => {
		if (!open) {
			draft = null;
			saveError = null;
			return;
		}
		if (settings && draft === null) {
			draft = toDraft(settings);
		} else if (!settings) {
			untrack(() => {
				if (!loading) load();
			});
		}
	});

	function discard() {
		if (settings) draft = toDraft(settings);
		saveError = null;
	}

	async function onsubmit(event: SubmitEvent) {
		event.preventDefault();
		if (!canSave) return;
		saving = true;
		saveError = null;
		try {
			const saved = await saveSettings(changes);
			if (open) draft = toDraft(saved);
		} catch (error) {
			saveError = error;
		} finally {
			saving = false;
		}
	}

	function errorText(error: unknown): string {
		if (error instanceof ApiError && !error.isLocal) return error.message;
		const code = error instanceof ApiError && error.isLocal ? (error.code as LocalErrorCode) : 'invalid_response';
		return $t(localErrorKey[code]);
	}

	interface LocaleOption extends DropdownOption {
		flagUrl: string | null;
	}

	// Idiomas descobertos em `locales/`, ordenados pelo nome nativo.
	const localeOptions = $derived(
		availableLocales
			.map((locale): LocaleOption => ({ value: locale.code, label: locale.name, flagUrl: locale.flagUrl }))
			.sort((a, b) => a.label.localeCompare(b.label, $localeStore))
	);

	function onlocalechange(code: string) {
		setLocale(code).catch((error: unknown) => console.warn('[settings] falha ao trocar o idioma', error));
	}

	const serverProfile = $derived($serverProfileStore);
	const sliderPercent = $derived(draft ? (draft.seconds - MIN_SECONDS) / (MAX_SECONDS - MIN_SECONDS) : 0);

	const inputClass =
		'h-11 w-full rounded-control border-[1.5px] border-inner-border bg-inner px-3.5 text-[14.5px] text-text outline-none focus:border-accent';
	const labelClass = 'text-[13.5px] leading-none font-semibold';
	const focusRingClass = 'has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-accent';
</script>

{#if open}
	<Dialog {onclose} closeOnBackdrop={!dirty}>
		{#snippet header(titleId)}
			<h2 id={titleId} class="text-[20px] leading-none font-semibold">{$t('settings.title')}</h2>
			<p class="text-label leading-[1.3] text-muted">{$t('settings.subtitle')}</p>
		{/snippet}

		{#if draft}
			<form id="{ids}-form" class="flex flex-col gap-[26px]" novalidate {onsubmit}>
				<div class="flex flex-col gap-2">
					<label for="{ids}-name" class={labelClass}>{$t('settings.instanceName')}</label>
					<input
						id="{ids}-name"
						class={[inputClass, nameInvalid && 'border-warn-border']}
						bind:value={draft.name}
						maxlength={NAME_MAX}
						required
						autocomplete="off"
						aria-invalid={nameInvalid ? 'true' : undefined}
						aria-describedby="{ids}-name-meta"
						data-autofocus
					/>
					<div id="{ids}-name-meta" class="flex justify-between gap-3">
						<span class="text-meta text-warn">
							{#if nameInvalid}{$t('settings.instanceNameRequired')}{/if}
						</span>
						<span class="num text-meta leading-none text-muted">{draft.name.length} / {NAME_MAX}</span>
					</div>
				</div>

				<div
					class="grid gap-[22px] border-t border-inner-border pt-[22px] lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)] lg:gap-5"
				>
					<div class="flex min-w-0 flex-col gap-2.5">
						<span id="{ids}-theme-label" class={labelClass}>{$t('settings.theme')}</span>
						<div
							role="radiogroup"
							aria-labelledby="{ids}-theme-label"
							class="grid grid-cols-3 gap-1 rounded-[11px] border border-inner-border bg-inner p-1"
						>
							{#each THEME_MODES as mode (mode)}
								{@const checked = draft.theme === mode}
								<label
									class={[
										'flex h-11 cursor-pointer items-center justify-center gap-1 rounded-small text-[13px] leading-none lg:h-[38px]',
										focusRingClass,
										checked ? 'bg-btn-primary font-semibold text-btn-primary-text' : 'font-medium text-text'
									]}
								>
									<input type="radio" class="sr-only" name="{ids}-theme" value={mode} bind:group={draft.theme} />
									{#if checked}<span aria-hidden="true">✓</span>{/if}
									{$t(themeKey[mode])}
								</label>
							{/each}
						</div>
					</div>

					<div class="flex min-w-0 flex-col gap-2.5">
						<span id="{ids}-language-label" class={labelClass}>{$t('settings.language')}</span>
						<Dropdown
							options={localeOptions}
							value={$localeStore}
							onchange={onlocalechange}
							labelledby="{ids}-language-label"
						>
							{#snippet icon(option)}
								{#if option.flagUrl}
									<img src={option.flagUrl} alt="" width="24" height="16" class="h-4 w-6 flex-none rounded-[3px] ring-1 ring-border" />
								{:else}
									<span
										class="num flex h-4 min-w-6 flex-none items-center justify-center rounded-[3px] border border-border px-0.5 text-[8px] leading-none text-muted"
										aria-hidden="true">{option.value}</span
									>
								{/if}
							{/snippet}
						</Dropdown>
						<span class="text-[12px] leading-[1.3] text-muted">{$t('settings.languageNote')}</span>
					</div>
				</div>

				<div class="flex items-center gap-4">
					<div class="flex flex-1 flex-col gap-1.5">
						<span id="{ids}-lite-label" class="text-[13.5px] leading-[1.2] font-semibold">{$t('settings.forceLite')}</span>
						<span id="{ids}-lite-hint" class="text-label leading-[1.45] text-muted">
							{$t('settings.forceLiteHint', {
								profile: serverProfile ? $t(profileKey[serverProfile.profile]) : $t('common.noValue')
							})}
						</span>
					</div>
					<div class="flex flex-none items-center gap-2">
						<span class="text-label leading-none font-medium text-muted" aria-hidden="true">
							{$t(draft.lite ? 'common.on' : 'common.off')}
						</span>
						<button
							type="button"
							role="switch"
							aria-checked={draft.lite}
							aria-labelledby="{ids}-lite-label"
							aria-describedby="{ids}-lite-hint"
							class={[
								'flex h-[26px] w-[46px] items-center rounded-[13px] border-[1.5px] p-0.5',
								draft.lite ? 'border-accent bg-btn-primary' : 'border-border bg-track'
							]}
							onclick={() => draft && (draft.lite = !draft.lite)}
						>
							<span
								class={[
									'size-[19px] rounded-full transition-transform',
									draft.lite ? 'translate-x-5 bg-btn-primary-text' : 'bg-text'
								]}
							></span>
						</button>
					</div>
				</div>

				<div class="flex flex-col gap-3 border-t border-inner-border pt-[22px]">
					<span id="{ids}-interval-label" class={labelClass}>{$t('settings.interval')}</span>
					<div role="radiogroup" aria-labelledby="{ids}-interval-label" class="flex flex-col gap-2">
						<label
							class={[
								'flex min-h-11 cursor-pointer items-center gap-3 rounded-control px-3.5',
								focusRingClass,
								draft.interval === 'auto' ? 'border-[1.5px] border-accent bg-accent-soft' : 'border border-inner-border'
							]}
						>
							<input type="radio" class="sr-only" name="{ids}-interval" value="auto" bind:group={draft.interval} />
							{@render radioMark(draft.interval === 'auto')}
							<span class="flex-1 py-2 text-[13.5px] leading-[1.3] font-medium">
								{$t('settings.intervalAuto')}
								<span class="text-muted">
									— {serverProfile
										? $t('settings.intervalAutoDetail', { n: serverProfile.recommended_poll_interval_s })
										: $t('settings.intervalAutoDetailPending')}
								</span>
							</span>
						</label>

						<div
							class={[
								'flex flex-col gap-2.5 rounded-control px-3.5 py-3',
								draft.interval === 'custom' ? 'border-[1.5px] border-accent bg-accent-soft' : 'border border-inner-border'
							]}
						>
							<label class={['flex cursor-pointer items-center gap-3 rounded-small', focusRingClass]}>
								<input type="radio" class="sr-only" name="{ids}-interval" value="custom" bind:group={draft.interval} />
								{@render radioMark(draft.interval === 'custom')}
								<span id="{ids}-custom-label" class="flex-1 text-[13.5px] leading-none font-medium">
									{$t('settings.intervalCustom')}
								</span>
								<span class={['num text-[13px] leading-none font-medium', draft.interval === 'custom' ? 'text-text' : 'text-muted']}>
									{$t('settings.seconds', { n: draft.seconds })}
								</span>
							</label>
							<div class={['flex flex-col gap-1.5 pl-7', draft.interval !== 'custom' && 'opacity-45']}>
								<div class="relative h-4">
									<input
										type="range"
										min={MIN_SECONDS}
										max={MAX_SECONDS}
										step="1"
										bind:value={draft.seconds}
										disabled={draft.interval !== 'custom'}
										aria-labelledby="{ids}-custom-label"
										aria-valuetext={$t('settings.seconds', { n: draft.seconds })}
										class="peer absolute inset-0 z-10 m-0 h-full w-full cursor-pointer opacity-0 disabled:cursor-not-allowed"
									/>
									<div class="absolute inset-x-0 top-1/2 h-1.5 -translate-y-1/2 rounded-[3px] bg-track">
										<div
											class={['h-full rounded-[3px]', draft.interval === 'custom' ? 'bg-accent' : 'bg-muted']}
											style:width="calc(8px + (100% - 16px) * {sliderPercent})"
										></div>
									</div>
									<span
										class="pointer-events-none absolute top-1/2 size-4 -translate-x-1/2 -translate-y-1/2 rounded-full bg-text peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-accent"
										style:left="calc(8px + (100% - 16px) * {sliderPercent})"
									></span>
								</div>
								<div class="num flex justify-between text-[11px] leading-none text-muted" aria-hidden="true">
									<span>{$t('settings.seconds', { n: MIN_SECONDS })}</span>
									<span>{$t('settings.seconds', { n: MAX_SECONDS })}</span>
								</div>
							</div>
						</div>
					</div>
				</div>
			</form>
		{:else if loadError}
			<div class="flex flex-col items-start gap-3" role="alert">
				<p class="text-body text-warn">{errorText(loadError)}</p>
				<button
					type="button"
					class="flex h-11 items-center rounded-[9px] border border-border px-4 text-button lg:h-10"
					onclick={load}
				>
					{$t('common.retry')}
				</button>
			</div>
		{:else}
			<p class="text-body text-muted" role="status">{$t('common.loading')}</p>
		{/if}

		{#snippet footer()}
			<span class="min-w-0 flex-1 text-label leading-[1.3]" aria-live="polite">
				{#if saving}
					<span class="text-muted">{$t('settings.saving')}</span>
				{:else if saveError}
					<span class="text-warn">{errorText(saveError)}</span>
				{:else if settings}
					<span class="text-muted">✓ {$t('settings.savedAt', { time: $formatters.time(settings.updated_at) })}</span>
				{/if}
			</span>
			<button
				type="button"
				class="flex h-11 items-center rounded-[9px] border border-border px-4 text-button disabled:opacity-45 lg:h-10"
				disabled={!dirty || saving}
				onclick={discard}
			>
				{$t('common.discard')}
			</button>
			<button
				type="submit"
				form="{ids}-form"
				class="flex h-11 items-center rounded-[9px] bg-btn-primary px-4 text-button text-btn-primary-text disabled:opacity-45 lg:h-10"
				disabled={!canSave || draft === null}
			>
				{$t('settings.saveChanges')}
			</button>
		{/snippet}
	</Dialog>
{/if}

{#snippet radioMark(checked: boolean)}
	<span
		class={[
			'size-4 flex-none rounded-full',
			checked ? 'border-[5px] border-accent' : 'border-[1.5px] border-muted'
		]}
		aria-hidden="true"
	></span>
{/snippet}
