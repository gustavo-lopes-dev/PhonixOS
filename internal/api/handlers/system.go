package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/collector"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/profile"
)

// System contém o perfil calculado no boot e o início do processo.
type System struct {
	Profile *profile.SystemProfile
	Started time.Time
}

func (h System) Health(c *fiber.Ctx) error {
	return success(c, http.StatusOK, struct {
		Status        string `json:"status"`
		UptimeSeconds int64  `json:"uptime_seconds"`
		Version       string `json:"version"`
	}{"healthy", int64(time.Since(h.Started).Seconds()), "1.0.0"})
}

func (h System) GetProfile(c *fiber.Ctx) error {
	return success(c, http.StatusOK, h.Profile)
}

func (h System) Metrics(c *fiber.Ctx) error {
	metrics, err := collector.CollectHardware()
	if err != nil {
		slog.Error("rest: hardware collector failed", "error", err)
		return failure(c, http.StatusInternalServerError, api.CodeHardwareCollectorFault, "Não foi possível coletar métricas do hardware.")
	}
	return success(c, http.StatusOK, metrics)
}
