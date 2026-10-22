// Envelopes REST e mensagens WebSocket — contracts.md §4.3 (§1 e §6).

export interface ApiResponse<T> {
	success: true;
	data: T;
	meta?: {
		total: number;
		limit: number;
		offset: number;
	};
}

export interface ApiErrorDetail {
	code: string;
	message: string;
	details?: unknown;
}

export interface ApiErrorResponse {
	success: false;
	error: ApiErrorDetail;
}

export type NetworkResponse<T> = ApiResponse<T> | ApiErrorResponse;

// Tipos de Mensagens WebSocket
export type WSAction = 'set_rate' | 'pause' | 'resume';

export interface WSClientMessage {
	action: WSAction;
	rate_ms?: number;
}

export type WSEventType = 'system_profile' | 'telemetry_tick';

export interface WSServerMessage<T = unknown> {
	event: WSEventType;
	payload: T;
}
