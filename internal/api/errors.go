package api

import (
	"errors"
	"net/http"

	"github.com/gustavo-lopes-dev/PhonixOS/internal/database"
)

// Catálogo padronizado de códigos de erro (contracts.md §7).
const (
	CodeValidationFailed       = "VALIDATION_FAILED"
	CodeInvalidJSONBody        = "INVALID_JSON_BODY"
	CodeShortcutNotFound       = "SHORTCUT_NOT_FOUND"
	CodeCardNotFound           = "CARD_NOT_FOUND"
	CodeDatabaseLocked         = "DATABASE_LOCKED"
	CodeDatabaseError          = "DATABASE_ERROR"
	CodeHardwareCollectorFault = "HARDWARE_COLLECTOR_FAULT"
	CodeInternalServerError    = "INTERNAL_SERVER_ERROR"
	CodeUpgradeRequired        = "UPGRADE_REQUIRED"
)

// CodeForError mapeia sentinelas da camada de persistência para o código e o
// status HTTP canônicos. Erros sem sentinela conhecida são tratados como falha
// de execução no banco (DATABASE_ERROR); erros de validação, de corpo JSON e
// de coletor são classificados pelo chamador diretamente nas constantes acima.
func CodeForError(err error) (code string, httpStatus int) {
	switch {
	case errors.Is(err, database.ErrShortcutNotFound):
		return CodeShortcutNotFound, http.StatusNotFound
	case errors.Is(err, database.ErrCardNotFound):
		return CodeCardNotFound, http.StatusNotFound
	case errors.Is(err, database.ErrDatabaseLocked):
		return CodeDatabaseLocked, http.StatusServiceUnavailable
	default:
		return CodeDatabaseError, http.StatusInternalServerError
	}
}
