// Estado de conexão do WebSocket de telemetria (handoff §State Management).
// Estado interno da UI em camelCase — não é payload. Escrito apenas por
// `services/websocket.ts`; componentes recebem só a leitura.

import { derived, readable, writable, type Readable } from 'svelte/store';
import { serverProfileStore } from '#lib/stores/profile.ts';

export type ConnectionStatus = 'connecting' | 'live' | 'stale';

export interface ConnectionState {
	/** `connecting` até o primeiro `telemetry_tick` da página; `stale` quando o socket cai depois dele. */
	state: ConnectionStatus;
	/** Horário local (ms) de recebimento do último `telemetry_tick`. */
	lastUpdateAt: number | null;
	/** Número da próxima tentativa desde a queda (1, 2, 3…); `0` com o socket aberto. */
	attempt: number;
	/** Horário local (ms) da próxima tentativa agendada; `null` sem timer pendente. */
	nextRetryAt: number | null;
}

export interface ConnectionWriter {
	set(state: ConnectionState): void;
	update(fn: (state: ConnectionState) => ConnectionState): void;
	/** Última `set_rate` pedida pela UI (`0` = perfil do servidor; `null` = nunca definida). */
	setRequestedRate(rateMs: number | null): void;
}

const initialState: ConnectionState = {
	state: 'connecting',
	lastUpdateAt: null,
	attempt: 0,
	nextRetryAt: null
};

const state = writable<ConnectionState>(initialState);
const requestedRateMs = writable<number | null>(null);
let writerClaimed = false;

export const connectionStore: Readable<ConnectionState> = { subscribe: state.subscribe };

/**
 * Entrega o acesso de escrita ao serviço WebSocket. Só pode ser obtido uma vez,
 * garantindo um único escritor do estado de conexão.
 */
export function claimConnectionWriter(): ConnectionWriter {
	if (writerClaimed) {
		throw new Error('connection_writer_claimed');
	}
	writerClaimed = true;
	return { set: state.set, update: state.update, setRequestedRate: requestedRateMs.set };
}

/** Relógio de 1 s, ativo apenas enquanto houver assinantes (sem timer ocioso). */
export const now: Readable<number> = readable(Date.now(), (set) => {
	set(Date.now());
	const id = setInterval(() => set(Date.now()), 1000);
	return () => clearInterval(id);
});

/** Segundos até a próxima tentativa ("Nova tentativa em 4 s"); `null` sem tentativa agendada. */
export const retryInSeconds: Readable<number | null> = derived([state, now], ([$state, $now]) =>
	$state.nextRetryAt === null ? null : Math.max(0, Math.ceil(($state.nextRetryAt - $now) / 1000))
);

/** Idade da última leitura em segundos ("há 38 s"); `null` antes do primeiro snapshot. */
export const lastUpdateAgeSeconds: Readable<number | null> = derived([state, now], ([$state, $now]) =>
	$state.lastUpdateAt === null ? null : Math.max(0, Math.floor(($now - $state.lastUpdateAt) / 1000))
);

/**
 * Cadência efetiva da sessão em segundos ("a cada 2 s"): a `set_rate` positiva pedida
 * pela UI ou, sem ela, `recommended_poll_interval_s` do perfil; `null` antes do perfil.
 */
export const updateIntervalSeconds: Readable<number | null> = derived(
	[requestedRateMs, serverProfileStore],
	([$rate, $profile]) => {
		if ($rate !== null && $rate > 0) return $rate / 1000;
		return $profile?.recommended_poll_interval_s ?? null;
	}
);
