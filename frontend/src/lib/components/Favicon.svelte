<script lang="ts">
	// Ícone de atalho: imagem de `icon_url` quando houver; sem ícone (ou falha ao carregar) →
	// inicial do título em caixa com borda tracejada 1.5 px `--muted` (handoff §A item 4).
	// Lançador: 34 px raio 9 (mobile 30/8); sidebar (3.3b): 22 px raio 6.

	interface Props {
		src: string | null;
		title: string;
		/** Lado em px (≥ 1024 px, ou sempre sem `mobileSize`). */
		size?: number;
		/** Raio em px. */
		radius?: number;
		/** Lado em px abaixo de 1024 px. */
		mobileSize?: number;
		mobileRadius?: number;
	}

	let { src, title, size = 34, radius = 9, mobileSize, mobileRadius }: Props = $props();

	let failedSrc = $state<string | null>(null);
	const showImage = $derived(src !== null && src !== '' && failedSrc !== src);
	const initial = $derived(Array.from(title.trim())[0]?.toLocaleUpperCase() ?? '');
</script>

{#if showImage}
	<img
		{src}
		alt=""
		width={size}
		height={size}
		loading="lazy"
		decoding="async"
		referrerpolicy="no-referrer"
		class="favicon flex-none object-contain"
		style:--fav="{size}px"
		style:--fav-m="{mobileSize ?? size}px"
		style:--fav-r="{radius}px"
		style:--fav-r-m="{mobileRadius ?? radius}px"
		onerror={() => (failedSrc = src)}
	/>
{:else}
	<span
		aria-hidden="true"
		class="favicon grid flex-none place-items-center border-[1.5px] border-dashed border-muted leading-none font-bold text-muted"
		style:--fav="{size}px"
		style:--fav-m="{mobileSize ?? size}px"
		style:--fav-r="{radius}px"
		style:--fav-r-m="{mobileRadius ?? radius}px"
	>
		{initial}
	</span>
{/if}

<style>
	.favicon {
		width: var(--fav-m);
		height: var(--fav-m);
		border-radius: var(--fav-r-m);
		font-size: calc(var(--fav-m) * 0.44);
	}

	@media (min-width: 1024px) {
		.favicon {
			width: var(--fav);
			height: var(--fav);
			border-radius: var(--fav-r);
			font-size: calc(var(--fav) * 0.44);
		}
	}
</style>
