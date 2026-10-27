<script lang="ts">
	// Linha de rede (handoff §A Cabeçalho, §B rodapé, §Interactions "Rede"):
	// `Rede · {interface} · {ipv4}[ · {hostname}]  ↓ Download {rx}  ↑ Upload {tx}`.
	// `header` = linha única Mono 12.5 do cabeçalho desktop; `footer` = duas linhas Mono 12 do
	// rodapé mobile. Taxas como "—" em `connecting` e até a segunda amostra; sem interface com
	// IPv4 → "Rede · —". Valores antigos (`stale`) a .55.
	import { browser } from '$app/env';
	import { t } from '#lib/i18n/index.ts';
	import { formatters } from '#lib/i18n/format.ts';
	import { connectionStore } from '#lib/stores/connection.ts';
	import { metricsStore, primaryInterface, rateBaselineReady } from '#lib/stores/metrics.ts';

	interface Props {
		variant?: 'header' | 'footer';
	}

	let { variant = 'header' }: Props = $props();

	const IPV4_LITERAL = /^\d{1,3}(?:\.\d{1,3}){3}$/;

	const iface = $derived(primaryInterface($metricsStore?.network));
	/** Nome da origem (ex.: `phonix.local` na Fase 5); omitido quando é IP literal ou o próprio IPv4. */
	const hostname = $derived.by(() => {
		if (!browser || iface === null) return '';
		const host = location.hostname;
		if (host === '' || host === iface.ipv4_address || IPV4_LITERAL.test(host) || host.includes(':')) return '';
		return host;
	});
	const ratesReady = $derived(iface !== null && $rateBaselineReady && $connectionStore.state !== 'connecting');
	const download = $derived(ratesReady && iface ? $formatters.rate(iface.bytes_recv_sec) : null);
	const upload = $derived(ratesReady && iface ? $formatters.rate(iface.bytes_sent_sec) : null);
	const stale = $derived($connectionStore.state === 'stale');
</script>

{#snippet address()}
	<span class="min-w-0">
		{$t('shell.network')} ·
		{#if iface}
			{iface.interface_name} · {iface.ipv4_address}{#if hostname}{' · '}{hostname}{/if}
		{:else}
			{$t('common.noValue')}
		{/if}
	</span>
{/snippet}

{#snippet rate(arrow: string, label: string, value: string | null)}
	<span class="whitespace-nowrap">
		<span aria-hidden="true">{arrow}</span>
		{label}
		{#if value}<b class="font-medium text-text">{value}</b>{:else}{$t('common.noValue')}{/if}
	</span>
{/snippet}

{#if variant === 'header'}
	<div class={['num flex flex-wrap items-center gap-x-5 gap-y-1.5 text-[12.5px] leading-none text-muted', stale && 'opacity-55']}>
		{@render address()}
		{@render rate('↓', $t('shell.download'), download)}
		{@render rate('↑', $t('shell.upload'), upload)}
	</div>
{:else}
	<div class={['num flex flex-col gap-1.5 px-1 pt-3 text-meta leading-[1.4] text-muted', stale && 'opacity-55']}>
		{@render address()}
		<span>
			{@render rate('↓', $t('shell.download'), download)} · {@render rate('↑', $t('shell.upload'), upload)}
		</span>
	</div>
{/if}
