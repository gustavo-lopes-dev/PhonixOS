// Último snapshot de telemetria e helpers puros de disponibilidade.
// Escrito apenas por `services/websocket.ts`; componentes recebem só a leitura.

import { derived, writable, type Readable } from 'svelte/store';
import type { BatteryMetrics, HardwareMetrics, NetworkMetrics } from '../../types/telemetry.ts';

export interface MetricsWriter {
	/** Registra um `telemetry_tick` recebido. */
	push(snapshot: HardwareMetrics): void;
}

const snapshot = writable<HardwareMetrics | null>(null);
const tickCount = writable(0);
let writerClaimed = false;

/** Última leitura recebida (mantida enquanto a conexão estiver `stale`). */
export const metricsStore: Readable<HardwareMetrics | null> = { subscribe: snapshot.subscribe };

/**
 * `true` a partir do segundo `telemetry_tick` desta página: a primeira leitura de
 * taxa de rede chega como `0` sem amostra anterior e deve ser exibida como "—".
 */
export const rateBaselineReady: Readable<boolean> = derived(tickCount, ($count) => $count >= 2);

/** Acesso de escrita do serviço WebSocket; só pode ser obtido uma vez. */
export function claimMetricsWriter(): MetricsWriter {
	if (writerClaimed) {
		throw new Error('metrics_writer_claimed');
	}
	writerClaimed = true;
	return {
		push(next) {
			snapshot.set(next);
			tickCount.update((count) => count + 1);
		}
	};
}

/** Bateria de contingência (`mock`) é indisponível: exibir "N/D", nunca `0 %`/`0 °C`. */
export function isBatteryAvailable(battery: BatteryMetrics): boolean {
	return battery.source !== 'mock';
}

function interfaceRank(name: string): number {
	if (name.startsWith('wlan')) return 0;
	if (name.startsWith('eth') || name.startsWith('rmnet')) return 1;
	return 2;
}

/**
 * Interface principal para a linha de rede: primeira com IPv4, preferindo `wlan*`,
 * depois `eth*`/`rmnet*`, preservando a ordem do servidor no empate; `null` se nenhuma.
 */
export function primaryInterface(network: readonly NetworkMetrics[] | null | undefined): NetworkMetrics | null {
	let best: NetworkMetrics | null = null;
	for (const iface of network ?? []) {
		if (iface.ipv4_address === '') continue;
		if (best === null || interfaceRank(iface.interface_name) < interfaceRank(best.interface_name)) {
			best = iface;
		}
	}
	return best;
}
