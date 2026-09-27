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
	"github.com/gustavo-lopes-dev/PhonixOS/internal/database"
)

// Settings vincula preferências e layout ao mesmo Store do servidor.
type Settings struct{ Store *database.Store }

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
	return nil
}

func validLength(value string, max int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= max
}

func (h Settings) Get(c *fiber.Ctx) error {
	settings, err := h.Store.GetSettings(c.UserContext())
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, settings)
}

func (h Settings) Patch(c *fiber.Ctx) error {
	var dto database.UpdateSettingsDTO
	if err := parseBody(c, &dto); err != nil {
		return err
	}
	if dto.InstanceName != nil && !validLength(*dto.InstanceName, 32) ||
		dto.ThemeMode != nil && *dto.ThemeMode != "dark" && *dto.ThemeMode != "light" && *dto.ThemeMode != "system" ||
		dto.CustomPollIntervalMS != nil && (*dto.CustomPollIntervalMS < 0 || *dto.CustomPollIntervalMS > 60000) ||
		dto.BatterySaverThreshold != nil && (*dto.BatterySaverThreshold < 50 || *dto.BatterySaverThreshold > 100) {
		return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "Configurações inválidas.")
	}
	settings, err := h.Store.UpdateSettings(c.UserContext(), dto)
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, settings)
}

func (h Settings) GetLayout(c *fiber.Ctx) error {
	cards, err := h.Store.ListLayoutCards(c.UserContext())
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, cards)
}

func (h Settings) PutLayout(c *fiber.Ctx) error {
	var body struct {
		Cards []database.UpdateLayoutCardDTO `json:"cards"`
	}
	if err := parseBody(c, &body); err != nil {
		return err
	}
	if body.Cards == nil {
		return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "O campo cards é obrigatório.")
	}
	seen := make(map[string]bool, len(body.Cards))
	for _, card := range body.Cards {
		if strings.TrimSpace(card.CardKey) == "" || card.PosX < 0 || card.PosY < 0 || card.Width <= 0 || card.Height <= 0 || seen[card.CardKey] {
			return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "Dados de layout inválidos.")
		}
		seen[card.CardKey] = true
	}
	cards, err := h.Store.UpdateLayoutCards(c.UserContext(), body.Cards)
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, cards)
}
