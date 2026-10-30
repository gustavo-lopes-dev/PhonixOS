<script lang="ts">
	// Casca de card do painel (handoff §A "Card base"): título UPPERCASE, selo opcional e corpo.
	// Desktop: padding 20/22, gap 18, min-height 244; mobile (< 1024 px): padding 16/18, gap 14.
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		/** Linha secundária sob o título (Sans 12.5 muted, ellipsis). */
		subtitle?: string;
		/** Texto do selo à direita ("Ao vivo" / "Antigo · 14:22:05"); omitido = sem selo. */
		badge?: string;
		badgeTone?: 'muted' | 'warn';
		/** Ponto à esquerda do selo: `live` verde pulsando, `offline` vermelho fixo. Reforço visual; o texto do selo carrega o estado. */
		badgeDot?: 'live' | 'offline';
		/** `min-height` de 244 px no desktop (cards de métrica). */
		fixedHeight?: boolean;
		/** Substitui o cabeçalho padrão; recebe o id do título para `aria-labelledby`. */
		header?: Snippet<[titleId: string]>;
		children: Snippet;
	}

	let { title, subtitle, badge, badgeTone = 'muted', badgeDot, fixedHeight = true, header, children }: Props = $props();

	const titleId = $props.id();
</script>

<section
	aria-labelledby={titleId}
	class={[
		'surface flex h-full flex-col gap-3.5 rounded-card px-[18px] pt-4 pb-[18px] lg:gap-[18px] lg:px-[22px] lg:py-5',
		fixedHeight && 'lg:min-h-[244px]'
	]}
>
	{#if header}
		{@render header(titleId)}
	{:else}
		<div class="flex items-center justify-between gap-2">
			<div class="flex min-w-0 flex-col gap-[7px]">
				<h2 id={titleId} class="text-card-title leading-[1.25] uppercase">{title}</h2>
				{#if subtitle}
					<span class="truncate text-label text-muted">{subtitle}</span>
				{/if}
			</div>
			{#if badge}
				<span
					class={[
						'inline-flex flex-none items-center gap-1.5 rounded-badge border px-2 py-[5px] text-meta leading-none font-medium whitespace-nowrap',
						badgeTone === 'warn' ? 'border-warn-border text-warn' : 'border-inner-border text-muted'
					]}
				>
					{#if badgeDot}
						<span
							aria-hidden="true"
							class={[
								'size-[7px] flex-none rounded-full',
								badgeDot === 'live' ? 'animate-live-pulse bg-ok' : 'bg-danger'
							]}
						></span>
					{/if}
					{badge}
				</span>
			{/if}
		</div>
	{/if}
	{@render children()}
</section>
