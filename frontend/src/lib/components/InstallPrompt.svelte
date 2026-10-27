<script lang="ts">
	// Chamada de instalação do app (handoff §Interactions "Instalar app"). A página só monta este
	// componente depois do `beforeinstallprompt` (store da 3.6); nunca é botão permanente.
	// `compact` = bloco no topo direito do cabeçalho desktop; `card` = cartão mobile accentSoft.
	import { t } from '#lib/i18n/index.ts';

	interface Props {
		variant: 'compact' | 'card';
		oninstall: () => void;
		/** "Agora não" (só no cartão). */
		ondismiss?: () => void;
	}

	let { variant, oninstall, ondismiss }: Props = $props();

	const titleId = $props.id();
</script>

{#if variant === 'compact'}
	<div class="flex items-center gap-3 rounded-tile border border-border bg-card py-2.5 pr-2.5 pl-4">
		<span class="text-label leading-[1.3] text-muted">{$t('shell.installAvailable')}</span>
		<button
			type="button"
			class="flex h-9 flex-none items-center rounded-[9px] bg-btn-primary px-3.5 text-button font-semibold text-btn-primary-text"
			onclick={oninstall}
		>
			{$t('shell.install')}
		</button>
	</div>
{:else}
	<section
		aria-labelledby={titleId}
		class="flex flex-col gap-3 rounded-card border border-accent-border bg-accent-soft p-4"
	>
		<div class="flex flex-col gap-1">
			<h2 id={titleId} class="text-[14.5px] leading-[1.2] font-semibold">{$t('shell.installTitle')}</h2>
			<p class="text-[13px] leading-[1.4] text-muted">{$t('shell.installDescription')}</p>
		</div>
		<div class="flex gap-2.5">
			<button
				type="button"
				class="flex h-11 flex-1 items-center justify-center rounded-control bg-btn-primary text-nav text-btn-primary-text"
				onclick={oninstall}
			>
				{$t('shell.install')}
			</button>
			{#if ondismiss}
				<button
					type="button"
					class="flex h-11 items-center rounded-control border border-border px-4 text-nav text-text"
					onclick={ondismiss}
				>
					{$t('shell.notNow')}
				</button>
			{/if}
		</div>
	</section>
{/if}
