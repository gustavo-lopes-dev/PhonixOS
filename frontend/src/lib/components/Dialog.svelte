<script lang="ts">
	// Diálogo modal reutilizável (handoff §D e §Interactions "Diálogo"): overlay, painel centralizado
	// (600 px padrão, `max-width: calc(100vw - 32px)`), cabeçalho/corpo/rodapé com separadores,
	// foco preso, Esc fecha e o foco volta ao gatilho. Montado só enquanto aberto (`{#if}` no pai).
	import { onMount, type Snippet } from 'svelte';
	import { t } from '#lib/i18n/index.ts';

	interface Props {
		/** Título (e subtítulo) do cabeçalho; o elemento do título deve usar `id={titleId}`. */
		header: Snippet<[titleId: string]>;
		children: Snippet;
		footer?: Snippet;
		/** Largura máxima do painel em px. */
		width?: number;
		/** Fecha ao clicar no véu (desligue em formulários para não perder o que foi digitado). */
		closeOnBackdrop?: boolean;
		onclose: () => void;
	}

	let { header, children, footer, width = 600, closeOnBackdrop = true, onclose }: Props = $props();

	const titleId = $props.id();
	let panel: HTMLDivElement;

	// i18n-ignore: seletor CSS de elementos focáveis, não é texto visível
	const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

	function focusables(): HTMLElement[] {
		return Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((el) => el.getClientRects().length > 0);
	}

	onMount(() => {
		const trigger = document.activeElement instanceof HTMLElement ? document.activeElement : null;
		const previousOverflow = document.body.style.overflow;
		document.body.style.overflow = 'hidden';
		(panel.querySelector<HTMLElement>('[data-autofocus]') ?? focusables()[0] ?? panel).focus();
		return () => {
			document.body.style.overflow = previousOverflow;
			if (trigger?.isConnected) trigger.focus();
		};
	});

	function onkeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			event.preventDefault();
			onclose();
			return;
		}
		if (event.key !== 'Tab') return;
		const items = focusables();
		const first = items[0];
		const last = items.at(-1);
		const active = document.activeElement;
		if (!first || !last) {
			event.preventDefault();
			panel.focus();
		} else if (event.shiftKey && (active === first || !panel.contains(active))) {
			event.preventDefault();
			last.focus();
		} else if (!event.shiftKey && (active === last || !panel.contains(active))) {
			event.preventDefault();
			first.focus();
		}
	}

	function onbackdrop(event: MouseEvent) {
		if (closeOnBackdrop && event.target === event.currentTarget) onclose();
	}
</script>

<svelte:window {onkeydown} />

<!-- O véu só repete o botão "Fechar" para ponteiro; teclado usa Esc. -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="fixed inset-0 z-50 flex items-center justify-center bg-overlay p-4" role="presentation" onclick={onbackdrop}>
	<div
		bind:this={panel}
		role="dialog"
		aria-modal="true"
		aria-labelledby={titleId}
		tabindex="-1"
		class="surface-dialog flex max-h-[calc(100dvh-32px)] w-full flex-col rounded-dialog outline-none"
		style:max-width="min({width}px, calc(100vw - 32px))"
	>
		<header class="flex items-center justify-between gap-4 border-b border-inner-border px-5 py-5 lg:px-7 lg:py-6">
			<div class="flex min-w-0 flex-col gap-[5px]">
				{@render header(titleId)}
			</div>
			<button
				type="button"
				class="flex h-11 flex-none items-center rounded-[9px] border border-border px-3 text-button lg:h-9"
				onclick={onclose}
			>
				{$t('common.close')}
			</button>
		</header>
		<div class="flex min-h-0 flex-1 flex-col gap-[26px] overflow-y-auto px-5 py-5 lg:px-7 lg:py-6">
			{@render children()}
		</div>
		{#if footer}
			<footer class="flex flex-wrap items-center gap-2.5 border-t border-inner-border px-5 py-4 lg:px-7 lg:py-[18px]">
				{@render footer()}
			</footer>
		{/if}
	</div>
</div>
