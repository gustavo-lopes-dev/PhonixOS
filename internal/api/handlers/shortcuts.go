package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/database"
)

// Shortcuts vincula o CRUD HTTP ao repositório de atalhos.
type Shortcuts struct{ Store *database.Store }

func shortcutID(c *fiber.Ctx) (int64, error) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fiber.NewError(http.StatusBadRequest, "ID do atalho deve ser inteiro positivo.")
	}
	return id, nil
}

func validURL(value string) bool {
	if strings.TrimSpace(value) != value {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Hostname() != "" && parsed.User == nil
}

func (h Shortcuts) List(c *fiber.Ctx) error {
	shortcuts, err := h.Store.ListShortcuts(c.UserContext())
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, shortcuts)
}

func (h Shortcuts) Create(c *fiber.Ctx) error {
	var dto database.CreateShortcutDTO
	if err := parseBody(c, &dto); err != nil {
		return err
	}
	if !validLength(dto.Title, 64) || !validURL(dto.URL) ||
		dto.IconURL != nil && !validURL(*dto.IconURL) ||
		dto.Category != nil && utf8.RuneCountInString(*dto.Category) > 32 {
		return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "Dados do atalho inválidos.")
	}
	shortcut, err := h.Store.CreateShortcut(c.UserContext(), dto)
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusCreated, shortcut)
}

func (h Shortcuts) Update(c *fiber.Ctx) error {
	id, err := shortcutID(c)
	if err != nil {
		return err
	}
	var dto database.UpdateShortcutDTO
	if err := parseBody(c, &dto); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(c.Body(), &fields); err != nil {
		return err
	}
	_, dto.IconURLPresent = fields["icon_url"]
	if dto.Title != nil && !validLength(*dto.Title, 64) ||
		dto.URL != nil && !validURL(*dto.URL) ||
		dto.IconURL != nil && !validURL(*dto.IconURL) ||
		dto.Category != nil && utf8.RuneCountInString(*dto.Category) > 32 {
		return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "Dados do atalho inválidos.")
	}
	shortcut, err := h.Store.UpdateShortcut(c.UserContext(), id, dto)
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, shortcut)
}

func (h Shortcuts) Delete(c *fiber.Ctx) error {
	id, err := shortcutID(c)
	if err != nil {
		return err
	}
	if err := h.Store.DeleteShortcut(c.UserContext(), id); err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, struct {
		ID      int64 `json:"id"`
		Deleted bool  `json:"deleted"`
	}{id, true})
}
