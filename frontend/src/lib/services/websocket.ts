// Cliente singleton do canal `/ws/telemetry` (contracts.md §6).
// Reconexão com backoff exponencial (1 s, 2 s, 4 s… teto 10 s), pausa/retomada pela
// Page Visibility API, reaplicação da última `set_rate` a cada conexão e alimentação de
// `connectionStore`, `metricsStore` e `serverProfileStore`. Expõe apenas estados e
// números; nenhum texto visível nasce aqui.

import { claimConnectionWriter, type ConnectionState } from '#lib/stores/connection.ts';
import { claimMetricsWriter } from '#lib/stores/metrics.ts';
import { serverProfileStore } from '#lib/stores/profile.ts';
import type { WSClientMessage, WSServerMessage } from '../../types/api.ts';
import type { HardwareMetrics, SystemProfile } from '../../types/telemetry.ts';

const WS_PATH = '/ws/telemetry';
const BASE_RETRY_DELAY_MS = 1000;
const MAX_RETRY_DELAY_MS = 10_000;
/** Piso do servidor para `set_rate` positivo (contracts.md §6.3). */
const MIN_RATE_MS = 1000;
const NORMAL_CLOSURE = 1000;

/** Atraso antes da tentativa `attempt` (1-based): 1 s, 2 s, 4 s, 8 s, 10 s, 10 s… */
export function retryDelayMs(attempt: number): number {
	return Math.min(BASE_RETRY_DELAY_MS * 2 ** Math.max(0, attempt - 1), MAX_RETRY_DELAY_MS);
}

/** URL na mesma origem do dashboard: `ws:` em HTTP, `wss:` em HTTPS (proxy da 4.0). */
export function telemetryUrl(origin: Pick<Location, 'protocol' | 'host'>): string {
	const scheme = origin.protocol === 'https:' ? 'wss:' : 'ws:';
	return `${scheme}//${origin.host}${WS_PATH}`;
}

function parseMessage(data: unknown): WSServerMessage | null {
	if (typeof data !== 'string') return null;
	try {
		const message: unknown = JSON.parse(data);
		if (typeof message !== 'object' || message === null) return null;
		const { event, payload } = message as Partial<WSServerMessage>;
		if (typeof event !== 'string' || typeof payload !== 'object' || payload === null) return null;
		return { event, payload } as WSServerMessage;
	} catch {
		return null;
	}
}

class TelemetrySocket {
	readonly #connection = claimConnectionWriter();
	readonly #metrics = claimMetricsWriter();

	#socket: WebSocket | null = null;
	#retryTimer: ReturnType<typeof setTimeout> | null = null;
	#active = false;
	#hasSnapshot = false;
	#lastUpdateAt: number | null = null;
	#attempt = 0;
	/** Última `set_rate` pedida pela UI; `null` enquanto a UI não definir nenhuma. */
	#requestedRateMs: number | null = null;

	/** Abre o canal e passa a reconectar automaticamente. Idempotente. */
	connect(): void {
		if (this.#active) return;
		this.#active = true;
		document.addEventListener('visibilitychange', this.#onVisibilityChange);
		this.#open();
	}

	/** Fecha o canal (Close 1000) e remove timers e listeners. Mantém a última leitura. */
	disconnect(): void {
		if (!this.#active) return;
		this.#active = false;
		document.removeEventListener('visibilitychange', this.#onVisibilityChange);
		this.#clearRetryTimer();
		const socket = this.#socket;
		this.#socket = null;
		if (socket) {
			this.#detach(socket);
			socket.close(NORMAL_CLOSURE);
		}
		this.#attempt = 0;
		this.#publish({ nextRetryAt: null });
	}

	/**
	 * Botão "Reconectar agora": antecipa a tentativa pendente. Não zera o backoff —
	 * se esta tentativa falhar, a próxima segue a progressão a partir de `attempt`.
	 */
	reconnectNow(): void {
		if (!this.#active || this.#socket) return;
		this.#clearRetryTimer();
		this.#open();
	}

	/**
	 * Aplica a cadência pedida pela UI (`0` restaura o perfil do servidor). O valor é
	 * guardado e reenviado em toda abertura/reconexão; antes do handshake, só é guardado.
	 * Valores fora do contrato (negativos, não inteiros, positivos < 1000 ms) são ignorados.
	 */
	setRate(rateMs: number): void {
		if (!Number.isInteger(rateMs) || rateMs < 0 || (rateMs > 0 && rateMs < MIN_RATE_MS)) {
			console.warn('[ws] set_rate ignorado:', rateMs);
			return;
		}
		this.#requestedRateMs = rateMs;
		this.#connection.setRequestedRate(rateMs);
		this.#send({ action: 'set_rate', rate_ms: rateMs });
	}

	#open(): void {
		this.#publish({ nextRetryAt: null });
		let socket: WebSocket;
		try {
			socket = new WebSocket(telemetryUrl(location));
		} catch (error) {
			console.warn('[ws] falha ao criar o socket:', error);
			this.#scheduleRetry();
			return;
		}
		this.#socket = socket;
		socket.onopen = () => {
			if (this.#socket !== socket) return;
			if (this.#requestedRateMs !== null) {
				this.#send({ action: 'set_rate', rate_ms: this.#requestedRateMs });
			}
			if (document.visibilityState === 'hidden') {
				this.#send({ action: 'pause' });
			}
		};
		socket.onmessage = (event: MessageEvent) => {
			if (this.#socket !== socket) return;
			this.#handleMessage(event.data);
		};
		// `error` sempre precede `close`; a reconexão é tratada só no `close`.
		socket.onclose = () => {
			if (this.#socket !== socket) return;
			this.#socket = null;
			this.#detach(socket);
			this.#scheduleRetry();
		};
	}

	#handleMessage(data: unknown): void {
		const message = parseMessage(data);
		if (!message) {
			console.warn('[ws] mensagem inválida descartada');
			return;
		}
		if (message.event === 'system_profile') {
			serverProfileStore.set(message.payload as SystemProfile);
		} else if (message.event === 'telemetry_tick') {
			this.#metrics.push(message.payload as HardwareMetrics);
			this.#hasSnapshot = true;
			this.#lastUpdateAt = Date.now();
			this.#attempt = 0;
			this.#publish({ nextRetryAt: null });
		}
	}

	#scheduleRetry(): void {
		if (!this.#active) return;
		this.#attempt += 1;
		const delay = retryDelayMs(this.#attempt);
		this.#publish({ nextRetryAt: Date.now() + delay });
		this.#retryTimer = setTimeout(() => {
			this.#retryTimer = null;
			this.#open();
		}, delay);
	}

	// Pausa por visibilidade não altera o estado de conexão (continua `live`).
	readonly #onVisibilityChange = (): void => {
		if (document.visibilityState === 'hidden') {
			this.#send({ action: 'pause' });
			return;
		}
		if (this.#socket) {
			this.#send({ action: 'resume' });
		} else if (this.#retryTimer !== null) {
			// De volta à aba com o servidor fora: tenta já, sem esperar o backoff.
			this.reconnectNow();
		}
	};

	#send(message: WSClientMessage): void {
		const socket = this.#socket;
		if (socket && socket.readyState === WebSocket.OPEN) {
			socket.send(JSON.stringify(message));
		}
	}

	#publish(patch: Pick<ConnectionState, 'nextRetryAt'>): void {
		const live = this.#socket?.readyState === WebSocket.OPEN && this.#hasSnapshot && this.#attempt === 0;
		this.#connection.set({
			state: live ? 'live' : this.#hasSnapshot ? 'stale' : 'connecting',
			lastUpdateAt: this.#lastUpdateAt,
			attempt: this.#attempt,
			nextRetryAt: patch.nextRetryAt
		});
	}

	#clearRetryTimer(): void {
		if (this.#retryTimer !== null) {
			clearTimeout(this.#retryTimer);
			this.#retryTimer = null;
		}
	}

	#detach(socket: WebSocket): void {
		socket.onopen = null;
		socket.onmessage = null;
		socket.onclose = null;
	}
}

/** Instância única do cliente de telemetria (a 3.6 chama `connect()`/`disconnect()`). */
export const telemetrySocket = new TelemetrySocket();
