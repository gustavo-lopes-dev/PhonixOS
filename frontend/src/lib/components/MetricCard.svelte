<script lang="ts">
	// Cards de CPU, RAM e Bateria/Temperatura (handoff §A itens 1–3, §B, §Interactions).
	// Estado de dado vem de `connectionStore`/`metricsStore`: `connecting` → skeleton; `live` → selo
	// "Ao vivo" com ponto verde pulsando; `stale` → valores a .55, medidores em `--muted` e selo
	// "Antigo · HH:MM:SS" com ponto vermelho fixo.
	// Bateria `mock` e campos fora do contrato (temperatura da CPU) aparecem como "N/D", nunca 0.
	// `summary` = resumo de uma linha do modo de edição do layout (sem a casca do card).
	import Card from '#lib/components/Card.svelte';
	import { formatters } from '#lib/i18n/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.ts';
	import { connectionStore } from '#lib/stores/connection.ts';
	import { isBatteryAvailable, metricsStore } from '#lib/stores/metrics.ts';
	import { serverProfileStore } from '#lib/stores/profile.ts';
	import type { BatteryMetrics } from '../../types/telemetry.ts';

	type MetricKind = 'cpu' | 'memory' | 'battery';
	type TemperatureLevel = 'normal' | 'elevated' | 'high';

	interface Props {
		kind: MetricKind;
		summary?: boolean;
	}

	let { kind, summary = false }: Props = $props();

	/** Circunferência do anel (r = 54). */
	const RING_LENGTH = 339.3;
	const TEMP_SCALE_MIN = 20;
	const TEMP_SCALE_MAX = 50;
	const CELSIUS = '°C';

	const titleKey: Record<MetricKind, MessageKey> = {
		cpu: 'metrics.cpuTitle',
		memory: 'metrics.memoryTitle',
		battery: 'metrics.batteryTitle'
	};
	const statusKey: Record<BatteryMetrics['status'], MessageKey> = {
		Charging: 'metrics.statusCharging',
		Discharging: 'metrics.statusDischarging',
		Full: 'metrics.statusFull',
		'Not charging': 'metrics.statusNotCharging',
		Unknown: 'metrics.statusUnknown'
	};
	const healthKey: Record<BatteryMetrics['health'], MessageKey> = {
		Good: 'metrics.healthGood',
		Overheat: 'metrics.healthOverheat',
		Dead: 'metrics.healthDead',
		'Over voltage': 'metrics.healthOverVoltage',
		Unspecified: 'metrics.healthUnspecified'
	};
	const temperatureKey: Record<TemperatureLevel, MessageKey> = {
		normal: 'metrics.tempNormal',
		elevated: 'metrics.tempElevated',
		high: 'metrics.tempHigh'
	};

	function temperatureLevel(battery: BatteryMetrics): TemperatureLevel {
		if (battery.health === 'Overheat' || battery.temperature_c >= 45) return 'high';
		if (battery.temperature_c >= 40) return 'elevated';
		return 'normal';
	}

	const clampPercent = (value: number) => Math.min(100, Math.max(0, value));

	const metrics = $derived($metricsStore);
	const connection = $derived($connectionStore);
	const waiting = $derived(connection.state === 'connecting' || metrics === null);
	const stale = $derived(connection.state === 'stale');

	const badge = $derived.by(() => {
		if (waiting || metrics === null) return undefined;
		if (!stale) return $t('connection.live');
		return $t('connection.stale', { time: $formatters.time(connection.lastUpdateAt ?? metrics.timestamp) });
	});

	// CPU: "{modelo} · {n} núcleos" (handoff §A item 3); sem modelo identificado, só os núcleos.
	const subtitle = $derived.by(() => {
		if (kind !== 'cpu' || waiting || metrics === null) return undefined;
		const cores = $t('metrics.cores', { count: metrics.cpu.core_count });
		const model = $serverProfileStore?.cpu_model;
		return model ? $t('metrics.cpuSubtitle', { model, cores }) : cores;
	});

	const cpuPercent = $derived(clampPercent(metrics?.cpu.usage_percent ?? 0));
	const memoryPercent = $derived(clampPercent(metrics?.memory.usage_percent ?? 0));
	const battery = $derived(metrics?.battery ?? null);
	const batteryAvailable = $derived(battery !== null && isBatteryAvailable(battery));
	const batteryLevel = $derived(clampPercent(battery?.level_percent ?? 0));
	const tempLevel = $derived(battery ? temperatureLevel(battery) : 'normal');
	const tempMarker = $derived(
		battery
			? clampPercent(((battery.temperature_c - TEMP_SCALE_MIN) / (TEMP_SCALE_MAX - TEMP_SCALE_MIN)) * 100)
			: 0
	);

	/** "6,4 de 16 GB": a unidade do valor usado só aparece quando difere da do total. */
	const memoryUsed = $derived.by(() => {
		if (!metrics) return null;
		const used = $formatters.bytesParts(metrics.memory.used_bytes);
		const total = $formatters.bytesParts(metrics.memory.total_bytes);
		return { used: used.unit === total.unit ? used.value : `${used.value} ${used.unit}`, total: `${total.value} ${total.unit}` };
	});

	const ringValueClass = 'text-ring-value-mobile lg:text-ring-value';
	const memoryValueClass = 'text-[32px] lg:text-value';
	const batteryValueClass = 'text-[28px] lg:text-value';
	const summaryValueClass = 'text-[34px]';

	const gaugeStroke = $derived(stale ? 'stroke-muted' : 'stroke-accent');
	const gaugeFill = $derived(stale ? 'bg-muted' : 'bg-accent');
	const batteryFill = $derived(stale ? 'bg-muted' : 'bg-ok');
</script>

{#snippet percentValue(value: number, sizeClass: string)}
	<span class="num leading-none font-semibold {sizeClass}">{$formatters.percent(value)}<span class="text-[0.5em]">%</span></span>
{/snippet}

{#if summary}
	<div class="flex flex-col gap-1.5">
		<span class="text-card-title uppercase">{$t(titleKey[kind])}</span>
		{#if waiting || metrics === null}
			<span class="text-body text-muted">{$t('connection.waitingFirstReading')}</span>
		{:else}
			<p class={['flex flex-wrap items-baseline gap-x-1.5 gap-y-1', stale && 'opacity-55']}>
				{#if kind === 'cpu'}
					{@render percentValue(cpuPercent, summaryValueClass)}
					<span class="text-body leading-[1.3] text-muted">
						{$t('metrics.cpuSummary', { load: $formatters.load(metrics.cpu.load_avg_1m) })} · {$t('metrics.cores', {
							count: metrics.cpu.core_count
						})}
					</span>
				{:else if kind === 'memory'}
					{@render percentValue(memoryPercent, summaryValueClass)}
					<span class="text-body leading-[1.3] text-muted">
						{$t('metrics.memorySummary', {
							used: $formatters.bytes(metrics.memory.used_bytes),
							total: $formatters.bytes(metrics.memory.total_bytes)
						})}
					</span>
				{:else if battery && batteryAvailable}
					{@render percentValue(batteryLevel, summaryValueClass)}
					<span class="text-body leading-[1.3] text-muted">
						{$t(statusKey[battery.status])} · {$formatters.temperature(battery.temperature_c)}
					</span>
				{:else}
					<span class="num text-[34px] leading-none font-semibold">{$t('common.notAvailable')}</span>
					<span class="text-body leading-[1.3] text-muted">{$t('common.unavailable')}</span>
				{/if}
			</p>
		{/if}
	</div>
{:else}
	<Card title={$t(titleKey[kind])} {subtitle} {badge} badgeTone={stale ? 'warn' : 'muted'} badgeDot={stale ? 'offline' : 'live'}>
		{#if waiting || metrics === null}
			<div class="flex items-center gap-[22px]" aria-hidden="true">
				<div class="size-24 flex-none rounded-full border-[10px] border-track lg:size-28"></div>
				<div class="flex flex-1 flex-col gap-3">
					<div class="h-3 w-[70%] rounded-[6px] bg-track"></div>
					<div class="h-[22px] w-[45%] rounded-[6px] bg-track"></div>
					<div class="h-3 w-[55%] rounded-[6px] bg-track"></div>
				</div>
			</div>
			<p class="mt-auto text-label leading-none text-muted">{$t('connection.waitingFirstReading')}</p>
		{:else if kind === 'cpu'}
			<div class={['flex items-center gap-5 lg:gap-6', stale && 'opacity-55']}>
				<div class="relative size-[108px] flex-none lg:size-[132px]">
					<svg viewBox="0 0 132 132" class="size-full -rotate-90" aria-hidden="true">
						<circle cx="66" cy="66" r="54" fill="none" stroke-width="11" class="stroke-track" />
						{#if cpuPercent > 0}
							<circle
								cx="66"
								cy="66"
								r="54"
								fill="none"
								stroke-width="11"
								stroke-linecap="round"
								stroke-dasharray="{(cpuPercent / 100) * RING_LENGTH} {RING_LENGTH}"
								class={gaugeStroke}
							/>
						{/if}
					</svg>
					<div class="absolute inset-0 flex flex-col items-center justify-center gap-[3px] lg:gap-1">
						{@render percentValue(cpuPercent, ringValueClass)}
						<span class="text-[11px] leading-none text-muted lg:text-meta lg:leading-none">{$t('metrics.inUse')}</span>
					</div>
				</div>
				<dl class="flex min-w-0 flex-1 flex-col gap-3 lg:gap-3.5">
					<div class="flex flex-col gap-1 lg:gap-[5px]">
						<dt class="text-meta leading-[1.2] text-muted lg:text-label">{$t('metrics.loadAverage')}</dt>
						<dd class="num text-value-secondary-mobile lg:text-value-secondary">
							{$formatters.load(metrics.cpu.load_avg_1m)}
						</dd>
					</div>
					<div class="flex flex-col gap-1 border-t border-inner-border pt-2.5 lg:gap-[5px] lg:pt-3">
						<dt class="text-meta leading-[1.2] text-muted lg:text-label">{$t('metrics.temperature')}</dt>
						<dd class="flex items-baseline gap-1.5 lg:gap-2">
							<span class="num text-value-secondary-mobile lg:text-value-secondary">{$t('common.notAvailable')}</span>
							<span class="text-[11.5px] leading-none font-medium text-muted lg:text-meta lg:leading-none">
								{$t('connection.noReading')}
							</span>
						</dd>
					</div>
				</dl>
			</div>
		{:else if kind === 'memory'}
			<div class={['flex flex-col gap-3 lg:gap-4', stale && 'opacity-55']}>
				<div class="flex items-baseline justify-between gap-3">
					<span class="flex items-baseline gap-2 lg:gap-2.5">
						{@render percentValue(memoryPercent, memoryValueClass)}
						<span class="text-label leading-none text-muted lg:text-body lg:leading-none">{$t('metrics.inUse')}</span>
					</span>
					{#if memoryUsed}
						<span class="num text-[14px] leading-none font-medium whitespace-nowrap lg:hidden">
							{memoryUsed.used} <span class="text-muted">{$t('metrics.of')}</span>
							{memoryUsed.total}
						</span>
					{/if}
				</div>
				<div class="flex flex-col gap-1.5">
					<div class="relative h-3 overflow-hidden rounded-[6px] bg-track" aria-hidden="true">
						<div class="h-full rounded-[6px] {gaugeFill}" style:width="{memoryPercent}%"></div>
						<span class="absolute inset-y-0 left-1/4 w-px bg-page-bg opacity-60 max-lg:hidden"></span>
						<span class="absolute inset-y-0 left-1/2 w-px bg-page-bg opacity-60"></span>
						<span class="absolute inset-y-0 left-3/4 w-px bg-page-bg opacity-60 max-lg:hidden"></span>
					</div>
					<div class="num flex justify-between text-[11px] leading-none text-muted max-lg:hidden">
						<span>0</span>
						<span>{$formatters.bytes(metrics.memory.total_bytes / 2)}</span>
						<span>{$formatters.bytes(metrics.memory.total_bytes)}</span>
					</div>
				</div>
				<dl class="grid grid-cols-3 gap-3 border-t border-inner-border pt-3 max-lg:hidden">
					<div class="flex min-w-0 flex-col gap-[5px]">
						<dt class="text-meta leading-none text-muted">{$t('metrics.used')}</dt>
						<dd class="num text-[17px] leading-none font-medium">{$formatters.bytes(metrics.memory.used_bytes)}</dd>
					</div>
					<div class="flex min-w-0 flex-col gap-[5px]">
						<dt class="text-meta leading-none text-muted">{$t('metrics.available')}</dt>
						<dd class="num text-[17px] leading-none font-medium">
							{$formatters.bytes(metrics.memory.available_bytes)}
						</dd>
					</div>
					<div class="flex min-w-0 flex-col gap-[5px]">
						<dt class="text-meta leading-none text-muted">{$t('metrics.total')}</dt>
						<dd class="num text-[17px] leading-none font-medium">{$formatters.bytes(metrics.memory.total_bytes)}</dd>
					</div>
				</dl>
				<p class="text-meta leading-none text-muted lg:hidden">
					{$t('metrics.availableInline')}
					<b class="num font-medium text-text">{$formatters.bytes(metrics.memory.available_bytes)}</b>
				</p>
			</div>
		{:else if battery}
			<div
				role="group"
				class={['grid grid-cols-2 gap-4', stale && 'opacity-55']}
				title={batteryAvailable ? undefined : $t('connection.noBatterySource')}
				aria-label={batteryAvailable ? undefined : $t('connection.noBatterySource')}
			>
				<div class="flex min-w-0 flex-col gap-2 lg:gap-3">
					<div class="flex items-center gap-2.5 lg:flex-col lg:items-start lg:gap-3">
						<span class="flex flex-none items-center" aria-hidden="true">
							<span class="h-5 w-10 rounded-[5px] border-2 border-text p-0.5 lg:h-7 lg:w-[58px] lg:rounded-[6px]">
								{#if batteryAvailable}
									<span class="block h-full rounded-[2px] lg:rounded-[3px] {batteryFill}" style:width="{batteryLevel}%"></span>
								{/if}
							</span>
							<span class="ml-0.5 h-2 w-[3px] rounded-r-[2px] bg-text lg:h-2.5 lg:w-1"></span>
						</span>
						{#if batteryAvailable}
							{@render percentValue(batteryLevel, batteryValueClass)}
						{:else}
							<span class="num text-[28px] leading-none font-semibold lg:text-value">{$t('common.notAvailable')}</span>
						{/if}
					</div>
					<span class="flex flex-col gap-1">
						{#if batteryAvailable}
							<span class="text-body leading-[1.2] font-semibold">{$t(statusKey[battery.status])}</span>
							<span class="text-meta leading-[1.2] text-muted">
								{$t(battery.is_plugged ? 'metrics.plugged' : 'metrics.onBattery')}<span class="max-lg:hidden"
									>{' · '}{$t(healthKey[battery.health])}</span
								>
							</span>
						{:else}
							<span class="text-body leading-[1.2] font-semibold">{$t('common.unavailable')}</span>
							<span class="text-meta leading-[1.2] text-muted">{$t('connection.noReadingSource')}</span>
						{/if}
					</span>
				</div>
				<div class="flex min-w-0 flex-col gap-2 border-l border-inner-border pl-4 lg:gap-3">
					<span class="text-meta leading-[1.2] text-muted lg:text-label">
						<span class="lg:hidden">{$t('metrics.temperature')}</span>
						<span class="max-lg:hidden">{$t('metrics.batteryTemperature')}</span>
					</span>
					<span class="num text-[24px] leading-none font-semibold whitespace-nowrap lg:text-[26px]">
						{batteryAvailable ? $formatters.temperature(battery.temperature_c) : $t('common.notAvailable')}
					</span>
					{#if batteryAvailable}
						<div class="flex flex-col gap-1.5 max-lg:hidden" aria-hidden="true">
							<div class="relative h-1.5 rounded-[3px] bg-track">
								<span
									class="absolute -top-1 h-3.5 w-[3px] rounded-[2px] bg-text"
									style:left="calc({tempMarker}% - 1.5px)"
								></span>
							</div>
							<div class="num flex justify-between text-[11px] leading-none text-muted">
								<span>{$formatters.percent(TEMP_SCALE_MIN)}</span>
								<span>{$formatters.percent(TEMP_SCALE_MAX)} {CELSIUS}</span>
							</div>
						</div>
						<span
							class={[
								'text-[12.5px] leading-[1.3] font-semibold lg:text-body lg:leading-[1.3] lg:font-semibold',
								tempLevel === 'high' && 'text-warn'
							]}
						>
							{$t(temperatureKey[tempLevel])}
						</span>
					{:else}
						<span class="text-[12.5px] leading-[1.3] font-semibold lg:text-body lg:leading-[1.3] lg:font-semibold">
							{$t('connection.noReading')}
						</span>
					{/if}
				</div>
			</div>
		{/if}
	</Card>
{/if}
