// Configurações do servidor (`/api/v1/settings`) e seus efeitos locais. Ao carregar ou salvar:
// `force_lite_mode` → `forcedLiteStore` (o perfil do servidor nunca é sobrescrito);
// `theme_mode` → classe `dark`/`light` no `<html>` + `<meta name="theme-color">` (`system`
// acompanha `prefers-color-scheme`); `custom_poll_interval_ms` → `set_rate` pelo serviço WS
// (inclusive `0`, que restaura a cadência do perfil). Tema e Lite são independentes.

import { browser } from '$app/env';
import { derived, writable, type Readable } from 'svelte/store';
import { api } from '#lib/services/api.ts';
import { telemetrySocket } from '#lib/services/websocket.ts';
import { forcedLiteStore, profileStore } from '#lib/stores/profile.ts';
import type { DashboardSettings, UpdateSettingsDTO } from '../../types/dashboard.ts';

export type ThemeMode = DashboardSettings['theme_mode'];
export type EffectiveTheme = 'dark' | 'light';

/** `--page-bg` de cada tema (app.css), espelhado na barra do navegador/PWA. */
const THEME_COLOR: Record<EffectiveTheme, string> = { dark: '#0f172a', light: '#f3f5f8' };
// i18n-ignore: media query CSS, não é texto visível
const DARK_QUERY ='(prefers-color-scheme: dark)';

/** Última versão conhecida do servidor; `null` até o primeiro `loadSettings()`. */
export const settingsStore = writable<DashboardSettings | null>(null);

/** Cadência efetiva em segundos: personalizada se > 0, senão a recomendada pelo perfil. */
export const effectivePollIntervalS: Readable<number | null> = derived(
	[settingsStore, profileStore],
	([$settings, $profile]) => {
		const custom = $settings?.custom_poll_interval_ms ?? 0;
		return custom > 0 ? custom / 1000 : $profile.recommendedPollIntervalS;
	}
);

let systemQuery: MediaQueryList | null = null;

function onSystemThemeChange(event: MediaQueryListEvent): void {
	setDocumentTheme(event.matches ? 'dark' : 'light');
}

function setDocumentTheme(theme: EffectiveTheme): void {
	const root = document.documentElement;
	root.classList.toggle('dark', theme === 'dark');
	root.classList.toggle('light', theme === 'light');
	document.querySelector('meta[name="theme-color"]')?.setAttribute('content', THEME_COLOR[theme]);
}

/** Aplica o tema no `<html>`; `system` segue o sistema até outro modo ser aplicado. */
export function applyTheme(mode: ThemeMode): void {
	if (!browser) return;
	if (mode === 'system') {
		if (!systemQuery) {
			systemQuery = matchMedia(DARK_QUERY);
			systemQuery.addEventListener('change', onSystemThemeChange);
		}
		setDocumentTheme(systemQuery.matches ? 'dark' : 'light');
		return;
	}
	if (systemQuery) {
		systemQuery.removeEventListener('change', onSystemThemeChange);
		systemQuery = null;
	}
	setDocumentTheme(mode);
}

function applySettings(settings: DashboardSettings): void {
	settingsStore.set(settings);
	forcedLiteStore.set(settings.force_lite_mode);
	applyTheme(settings.theme_mode);
	// O serviço guarda a preferência e a reenvia a cada abertura/reconexão do socket.
	telemetrySocket.setRate(settings.custom_poll_interval_ms);
}

// Respostas fora de ordem (ex.: carga lenta concluída após um save) não sobrescrevem a mais nova.
let latestRequest = 0;

async function track(call: () => Promise<DashboardSettings>): Promise<DashboardSettings> {
	const id = ++latestRequest;
	const settings = await call();
	if (id === latestRequest) applySettings(settings);
	return settings;
}

/** Campos editáveis pela UI; `battery_saver_threshold` fica fora por decisão de produto (handoff §D). */
export type EditableSettings = Pick<
	DashboardSettings,
	'instance_name' | 'theme_mode' | 'force_lite_mode' | 'custom_poll_interval_ms'
>;

/** Corpo do `PATCH` com **somente** os campos editáveis que diferem do servidor. */
export function settingsChanges(current: DashboardSettings, next: EditableSettings): UpdateSettingsDTO {
	const dto: UpdateSettingsDTO = {};
	if (next.instance_name !== current.instance_name) dto.instance_name = next.instance_name;
	if (next.theme_mode !== current.theme_mode) dto.theme_mode = next.theme_mode;
	if (next.force_lite_mode !== current.force_lite_mode) dto.force_lite_mode = next.force_lite_mode;
	if (next.custom_poll_interval_ms !== current.custom_poll_interval_ms) {
		dto.custom_poll_interval_ms = next.custom_poll_interval_ms;
	}
	return dto;
}

/** `GET /api/v1/settings` e aplica os efeitos. Rejeita com `ApiError`. */
export function loadSettings(): Promise<DashboardSettings> {
	return track(() => api.getSettings());
}

/** `PATCH /api/v1/settings` com os campos alterados e aplica a resposta. Rejeita com `ApiError`. */
export function saveSettings(dto: UpdateSettingsDTO): Promise<DashboardSettings> {
	return track(() => api.updateSettings(dto));
}
