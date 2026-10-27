<script lang="ts">
	// Raiz da aplicação: único ponto que aplica `.lite-mode` e `lang`/`dir` no `<html>` (o tema
	// `.dark`/`.light` é de `settings.ts`). O Service Worker é registrado automaticamente pelo
	// SvelteKit (`serviceWorker.register` em vite.config.ts) — não registrar aqui.
	import '../app.css';
	import { onMount, type Snippet } from 'svelte';
	import { initI18n } from '#lib/i18n/index.ts';
	import { listenForInstall } from '#lib/stores/install.ts';
	import { localeMeta, localeStore } from '#lib/stores/locale.ts';
	import { profileStore } from '#lib/stores/profile.ts';

	let { children }: { children: Snippet } = $props();

	// Os filhos só renderizam com os catálogos carregados (sem chaves cruas no primeiro paint);
	// enquanto isso, o fundo `--page-bg` do `app.css` já está aplicado. Falha não bloqueia a UI.
	const i18nReady = initI18n().catch((error: unknown) => {
		console.warn('[i18n] falha ao inicializar os catálogos', error);
	});

	$effect(() => {
		document.documentElement.classList.toggle('lite-mode', $profileStore.isLite);
	});

	$effect(() => {
		const root = document.documentElement;
		root.lang = $localeStore;
		root.dir = $localeMeta.dir;
	});

	onMount(() => listenForInstall());
</script>

<svelte:head>
	<title>PhonixOS</title>
</svelte:head>

{#await i18nReady then}
	{@render children()}
{/await}
