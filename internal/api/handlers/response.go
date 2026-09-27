package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api"
)

func success[T any](c *fiber.Ctx, status int, data T) error {
	return c.Status(status).JSON(api.APIResponse[T]{Success: true, Data: data})
}

func failure(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(api.APIErrorResponse{Success: false, Error: api.APIErrorDetail{Code: code, Message: message}})
}

func databaseFailure(c *fiber.Ctx, err error) error {
	code, status := api.CodeForError(err)
	slog.Error("rest: database operation failed", "code", code, "error", err)
	switch code {
	case api.CodeShortcutNotFound:
		return failure(c, status, code, "Atalho com o ID especificado não foi encontrado.")
	case api.CodeCardNotFound:
		return failure(c, status, code, "Card solicitado não foi encontrado.")
	case api.CodeDatabaseLocked:
		return failure(c, status, code, "Banco de dados temporariamente ocupado.")
	default:
		return failure(c, status, code, "Não foi possível acessar o banco de dados.")
	}
}

// parseBody distingue JSON inválido de campos/tipos inválidos e exige JSON no corpo.
// Apenas icon_url pode ser explicitamente null (para remover o ícone).
func parseBody[T any](c *fiber.Ctx, dto *T) error {
	mediaType, _, err := mime.ParseMediaType(c.Get(fiber.HeaderContentType))
	if err != nil || !strings.EqualFold(mediaType, fiber.MIMEApplicationJSON) {
		return fiber.NewError(http.StatusBadRequest, "Content-Type deve ser application/json.")
	}
	trimmed := bytes.TrimSpace(c.Body())
	if len(trimmed) > 0 && trimmed[0] != '{' {
		if json.Valid(trimmed) {
			return fiber.NewError(http.StatusBadRequest, "O corpo deve conter um objeto JSON.")
		}
		return api.InvalidJSONError{}
	}
	decoder := json.NewDecoder(bytes.NewReader(c.Body()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dto); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return api.InvalidJSONError{}
		}
		return fiber.NewError(http.StatusBadRequest, "Campos do corpo inválidos.")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return api.InvalidJSONError{}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(c.Body(), &fields); err != nil {
		return api.InvalidJSONError{}
	}
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && name != "icon_url" {
			return fiber.NewError(http.StatusBadRequest, "Campos do corpo não podem ser null.")
		}
	}
	return nil
}

func validLength(value string, max int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= max
}
