// Instalação da PWA (handoff §Interactions "Instalar app"): a ação só existe depois do
// `beforeinstallprompt`, nunca como botão permanente. O evento é guardado e usado uma única
// vez; "Agora não" oculta a chamada até o fim da sessão. Os listeners são instalados pelo
// `+layout.svelte` via `listenForInstall()` (no `onMount`), que devolve a remoção.

import { browser } from '$app/env';
import { derived, get, writable, type Readable } from 'svelte/store';

/** Evento não padronizado (Chromium); ausente da lib DOM do TypeScript. */
interface BeforeInstallPromptEvent extends Event {
	prompt(): Promise<void>;
	readonly userChoice: Promise<{ outcome: 'accepted' | 'dismissed'; platform: string }>;
}

const DISMISSED_KEY = 'phonix.installDismissed';
// i18n-ignore: media query CSS, não é texto visível
const STANDALONE_QUERY = '(display-mode: standalone)';

function readDismissed(): boolean {
	if (!browser) return false;
	try {
		return sessionStorage.getItem(DISMISSED_KEY) === '1';
	} catch {
		return false;
	}
}

const deferredPrompt = writable<BeforeInstallPromptEvent | null>(null);
const dismissed = writable<boolean>(readDismissed());

/** `true` enquanto há um `beforeinstallprompt` guardado e o usuário não o dispensou. */
export const installAvailable: Readable<boolean> = derived(
	[deferredPrompt, dismissed],
	([$event, $dismissed]) => $event !== null && !$dismissed
);

/**
 * Abre o diálogo nativo de instalação (uma única vez por evento; deve partir de um gesto do
 * usuário). Resolve `true` se aceito; `false` se recusado, indisponível ou com falha.
 */
export async function promptInstall(): Promise<boolean> {
	const event = get(deferredPrompt);
	if (!event) return false;
	deferredPrompt.set(null);
	try {
		await event.prompt();
		const { outcome } = await event.userChoice;
		return outcome === 'accepted';
	} catch (error) {
		console.warn('[install] falha ao abrir o prompt de instalação', error);
		return false;
	}
}

/** "Agora não": oculta a chamada até o fim da sessão (`sessionStorage`). */
export function dismissInstall(): void {
	dismissed.set(true);
	try {
		sessionStorage.setItem(DISMISSED_KEY, '1');
	} catch {
		// Armazenamento indisponível: a dispensa vale até recarregar a página.
	}
}

/**
 * Captura `beforeinstallprompt` (suprimindo a mini-barra do navegador) e limpa o evento em
 * `appinstalled` ou ao passar para `display-mode: standalone`. Devolve a remoção dos listeners.
 */
export function listenForInstall(): () => void {
	if (!browser) return () => {};
	const standalone = matchMedia(STANDALONE_QUERY);

	const onBeforeInstallPrompt = (event: Event) => {
		event.preventDefault();
		if (standalone.matches) return;
		deferredPrompt.set(event as BeforeInstallPromptEvent);
	};
	const onAppInstalled = () => deferredPrompt.set(null);
	const onDisplayModeChange = () => {
		if (standalone.matches) deferredPrompt.set(null);
	};

	window.addEventListener('beforeinstallprompt', onBeforeInstallPrompt);
	window.addEventListener('appinstalled', onAppInstalled);
	standalone.addEventListener('change', onDisplayModeChange);

	return () => {
		window.removeEventListener('beforeinstallprompt', onBeforeInstallPrompt);
		window.removeEventListener('appinstalled', onAppInstalled);
		standalone.removeEventListener('change', onDisplayModeChange);
		deferredPrompt.set(null);
	};
}
