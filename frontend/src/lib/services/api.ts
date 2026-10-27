// Cliente HTTP tipado da API REST (contracts.md §1 e §5), na mesma origem do dashboard.
// Desembrulha o envelope de sucesso (`data`) e converte o envelope de erro — ou falhas locais
// de rede, timeout e parsing — em `ApiError`. Expõe apenas códigos e dados; nenhum texto
// visível nasce aqui (a UI traduz os códigos locais e exibe o `message` do servidor).

import type { ApiErrorResponse, ApiResponse, NetworkResponse } from '../../types/api.ts';
import type {
	CreateShortcutDTO,
	DashboardSettings,
	LayoutCard,
	Shortcut,
	UpdateLayoutCardDTO,
	UpdateSettingsDTO,
	UpdateShortcutDTO
} from '../../types/dashboard.ts';

const API_PREFIX = '/api/v1';
const REQUEST_TIMEOUT_MS = 10_000;

/** Falhas sem resposta válida do servidor; o servidor usa códigos em maiúsculas (§7). */
export type LocalErrorCode = 'network_error' | 'timeout' | 'invalid_response';

const LOCAL_ERROR_CODES: readonly string[] = ['network_error', 'timeout', 'invalid_response'] satisfies LocalErrorCode[];

export function isLocalErrorCode(code: string): code is LocalErrorCode {
	return LOCAL_ERROR_CODES.includes(code);
}

/**
 * Erro de chamada REST. `code` é o código canônico do envelope (`VALIDATION_FAILED`…) ou um
 * `LocalErrorCode`; `message` é o texto do servidor (ou o próprio código, se local);
 * `status` é `null` quando não houve resposta HTTP.
 */
export class ApiError extends Error {
	readonly code: string;
	readonly status: number | null;
	readonly details: unknown;

	constructor(code: string, message: string, status: number | null, details?: unknown) {
		super(message);
		this.name = new.target.name;
		this.code = code;
		this.status = status;
		this.details = details;
	}

	get isLocal(): boolean {
		return isLocalErrorCode(this.code);
	}
}

function localError(code: LocalErrorCode, status: number | null = null, cause?: unknown): ApiError {
	return new ApiError(code, code, status, cause);
}

function isErrorEnvelope(body: unknown): body is ApiErrorResponse {
	if (typeof body !== 'object' || body === null) return false;
	const { success, error } = body as Partial<ApiErrorResponse>;
	return (
		success === false &&
		typeof error === 'object' &&
		error !== null &&
		typeof error.code === 'string' &&
		typeof error.message === 'string'
	);
}

function isSuccessEnvelope<T>(body: unknown): body is ApiResponse<T> {
	return typeof body === 'object' && body !== null && (body as Partial<ApiResponse<T>>).success === true && 'data' in body;
}

type Method = 'get' | 'post' | 'put' | 'patch' | 'delete';

async function request<T>(method: Method, path: string, body?: unknown): Promise<T> {
	let response: Response;
	try {
		response = await fetch(`${API_PREFIX}${path}`, {
			// `fetch` só normaliza a caixa de GET/POST/PUT/DELETE; PATCH precisa ir em maiúsculas.
			method: method.toUpperCase(),
			headers: body === undefined ? { accept: 'application/json' } : { accept: 'application/json', 'content-type': 'application/json' },
			body: body === undefined ? undefined : JSON.stringify(body),
			signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS)
		});
	} catch (error) {
		const timedOut = error instanceof DOMException && error.name === 'TimeoutError';
		throw localError(timedOut ? 'timeout' : 'network_error', null, error);
	}

	let parsed: unknown;
	try {
		parsed = await response.json();
	} catch (error) {
		const timedOut = error instanceof DOMException && error.name === 'TimeoutError';
		throw localError(timedOut ? 'timeout' : 'invalid_response', response.status, error);
	}

	const envelope = parsed as NetworkResponse<T>;
	if (isErrorEnvelope(envelope)) {
		throw new ApiError(envelope.error.code, envelope.error.message, response.status, envelope.error.details);
	}
	if (!response.ok || !isSuccessEnvelope<T>(envelope)) {
		throw localError('invalid_response', response.status);
	}
	return envelope.data;
}

/** Resposta de `DELETE /api/v1/shortcuts/:id` (contracts.md §5.2). */
export interface DeleteShortcutResult {
	id: number;
	deleted: boolean;
}

export const api = {
	// §5.4 — configurações (singleton)
	getSettings: (): Promise<DashboardSettings> => request('get', '/settings'),
	updateSettings: (dto: UpdateSettingsDTO): Promise<DashboardSettings> => request('patch', '/settings', dto),

	// §5.3 — layout (lote completo)
	getLayout: (): Promise<LayoutCard[]> => request('get', '/layout'),
	updateLayout: (cards: UpdateLayoutCardDTO[]): Promise<LayoutCard[]> => request('put', '/layout', { cards }),

	// §5.2 — atalhos
	listShortcuts: (): Promise<Shortcut[]> => request('get', '/shortcuts'),
	createShortcut: (dto: CreateShortcutDTO): Promise<Shortcut> => request('post', '/shortcuts', dto),
	updateShortcut: (id: number, dto: UpdateShortcutDTO): Promise<Shortcut> => request('put', `/shortcuts/${id}`, dto),
	deleteShortcut: (id: number): Promise<DeleteShortcutResult> => request('delete', `/shortcuts/${id}`)
};
