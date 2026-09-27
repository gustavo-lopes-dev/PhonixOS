package handlers

import (
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/database"
)

// Settings vincula as preferências HTTP ao Store do servidor.
type Settings struct{ Store *database.Store }

const (
	minCustomPollIntervalMS = 1000
	maxCustomPollIntervalMS = 60000
)

func validCustomPollInterval(value int) bool {
	return value == 0 || value >= minCustomPollIntervalMS && value <= maxCustomPollIntervalMS
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
		dto.CustomPollIntervalMS != nil && !validCustomPollInterval(*dto.CustomPollIntervalMS) ||
		dto.BatterySaverThreshold != nil && (*dto.BatterySaverThreshold < 50 || *dto.BatterySaverThreshold > 100) {
		return failure(c, http.StatusBadRequest, api.CodeValidationFailed, "Configurações inválidas.")
	}
	settings, err := h.Store.UpdateSettings(c.UserContext(), dto)
	if err != nil {
		return databaseFailure(c, err)
	}
	return success(c, http.StatusOK, settings)
}
