// Chave de mensagem para uma falha de chamada REST (contracts.md §7). O texto do envelope vem
// sempre em pt-BR, então a UI traduz pelo código canônico; códigos desconhecidos caem numa
// mensagem genérica que mostra o código. Sem texto visível aqui: só chaves e parâmetros.

import { ApiError, type LocalErrorCode } from '#lib/services/api.ts';
import type { MessageKey } from '#lib/i18n/index.ts';

const localErrorKey: Record<LocalErrorCode, MessageKey> = {
	network_error: 'common.errorNetwork',
	timeout: 'common.errorTimeout',
	invalid_response: 'common.errorInvalidResponse'
};

const serverErrorKey: Record<string, MessageKey> = {
	VALIDATION_FAILED: 'common.errorValidationFailed',
	INVALID_JSON_BODY: 'common.errorInvalidJsonBody',
	PAYLOAD_TOO_LARGE: 'common.errorPayloadTooLarge',
	SHORTCUT_NOT_FOUND: 'common.errorShortcutNotFound',
	CARD_NOT_FOUND: 'common.errorCardNotFound',
	DATABASE_LOCKED: 'common.errorDatabaseLocked',
	DATABASE_ERROR: 'common.errorDatabase',
	HARDWARE_COLLECTOR_FAULT: 'common.errorHardwareCollector',
	INTERNAL_SERVER_ERROR: 'common.errorInternal'
};

export interface ErrorMessage {
	key: MessageKey;
	params?: { code: string };
}

/** Mensagem traduzível para qualquer erro lançado por `api` (ou desconhecido). */
export function errorMessage(error: unknown): ErrorMessage {
	if (!(error instanceof ApiError)) return { key: localErrorKey.invalid_response };
	if (error.isLocal) return { key: localErrorKey[error.code as LocalErrorCode] };
	const key = serverErrorKey[error.code];
	return key ? { key } : { key: 'common.errorServer', params: { code: error.code } };
}
