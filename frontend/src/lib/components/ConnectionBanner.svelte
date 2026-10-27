<script lang="ts">
	// Faixa de conexão interrompida (handoff §Interactions "Estado de conexão"; telas 4b/4d).
	// Só existe em `stale`. Contagem regressiva derivada de `nextRetryAt` pelo relógio de 1 s de
	// `connection.ts` (o timer para ao desmontar, quando não há mais assinantes). Sem tentativa
	// agendada (socket reaberto aguardando tick, ou tentativa em curso) exibe "Reconectando…".
	// "Reconectar agora" antecipa a tentativa pendente no serviço WS sem zerar o backoff.
	import { t } from '#lib/i18n/index.ts';
	import { formatters } from '#lib/i18n/format.ts';
	import { telemetrySocket } from '#lib/services/websocket.ts';
	import { connectionStore, retryInSeconds } from '#lib/stores/connection.ts';
	import { metricsStore } from '#lib/stores/metrics.ts';

	const lastReadingAt = $derived($connectionStore.lastUpdateAt ?? $metricsStore?.timestamp ?? null);
	const time = $derived(lastReadingAt === null ? $t('common.noValue') : $formatters.time(lastReadingAt));
	const retryIn = $derived($retryInSeconds);
	const detail = $derived(
		retryIn === null
			? $t('shell.bannerReconnecting', { time })
			: $t('shell.bannerDetail', { n: retryIn, k: $connectionStore.attempt, time })
	);
	const detailShort = $derived(
		retryIn === null
			? $t('shell.bannerReconnectingShort', { time })
			: $t('shell.bannerDetailShort', { n: retryIn, time })
	);
</script>

{#if $connectionStore.state === 'stale'}
	<div role="status">
		<!-- Desktop (≥ 1024 px) -->
		<div class="hidden items-center gap-4 rounded-[14px] border border-warn-border bg-warn-soft py-3.5 pr-4 pl-[18px] lg:flex">
			<span
				aria-hidden="true"
				class="grid size-[26px] flex-none place-items-center rounded-full border-2 border-warn text-[14px] leading-none font-bold text-warn"
			>
				!
			</span>
			<div class="flex min-w-0 flex-1 flex-col gap-1">
				<span class="text-[14px] leading-[1.2] font-semibold text-text">{$t('shell.bannerTitle')}</span>
				<!-- A contagem muda a cada segundo: fora da região viva para não ser anunciada em loop. -->
				<span aria-live="off" class="text-[13px] leading-[1.4] text-muted">{detail}</span>
			</div>
			<button
				type="button"
				class="flex h-9 flex-none items-center rounded-[9px] border border-border bg-inner px-3.5 text-button font-semibold text-text"
				onclick={() => telemetrySocket.reconnectNow()}
			>
				{$t('connection.reconnectNow')}
			</button>
		</div>

		<!-- Mobile (< 1024 px) -->
		<div class="flex gap-3 rounded-[14px] border border-warn-border bg-warn-soft p-3.5 lg:hidden">
			<span
				aria-hidden="true"
				class="grid size-6 flex-none place-items-center rounded-full border-2 border-warn text-[13px] leading-none font-bold text-warn"
			>
				!
			</span>
			<div class="flex min-w-0 flex-col gap-2.5">
				<p class="text-[13px] leading-[1.4] text-muted">
					<b class="font-semibold text-text">{$t('shell.bannerTitleShort')}</b>
					<span aria-live="off">{detailShort}</span>
				</p>
				<button
					type="button"
					class="flex h-11 items-center self-start rounded-control border border-border bg-inner px-3.5 text-button font-semibold text-text"
					onclick={() => telemetrySocket.reconnectNow()}
				>
					{$t('connection.reconnectNow')}
				</button>
			</div>
		</div>
	</div>
{/if}
