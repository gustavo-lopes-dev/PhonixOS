package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// Routes define os handlers REST injetados pelo ponto de entrada.
type Routes struct {
	Health, Profile, Metrics       fiber.Handler
	ListShortcuts, CreateShortcut  fiber.Handler
	UpdateShortcut, DeleteShortcut fiber.Handler
	GetSettings, PatchSettings     fiber.Handler
	GetLayout, PutLayout           fiber.Handler
}

// NewServer configura o Fiber e registra todas as rotas REST da versão 1.
func NewServer(routes Routes) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: serverError})
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{Format: "${time} ${status} ${method} ${path}\n"}))
	app.Use(cors.New(cors.Config{AllowOrigins: "*", AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS", AllowHeaders: "Origin,Content-Type,Accept"}))

	apiV1 := app.Group("/api/v1")
	apiV1.Get("/health", routes.Health)
	apiV1.Get("/system/profile", routes.Profile)
	apiV1.Get("/system/metrics", routes.Metrics)
	apiV1.Get("/shortcuts", routes.ListShortcuts)
	apiV1.Post("/shortcuts", routes.CreateShortcut)
	apiV1.Put("/shortcuts/:id", routes.UpdateShortcut)
	apiV1.Delete("/shortcuts/:id", routes.DeleteShortcut)
	apiV1.Get("/settings", routes.GetSettings)
	apiV1.Patch("/settings", routes.PatchSettings)
	apiV1.Get("/layout", routes.GetLayout)
	apiV1.Put("/layout", routes.PutLayout)
	return app
}

func serverError(c *fiber.Ctx, err error) error {
	status, code, message := http.StatusInternalServerError, CodeInternalServerError, "Erro interno do servidor."
	var invalidJSON InvalidJSONError
	var fiberErr *fiber.Error
	switch {
	case errors.As(err, &invalidJSON):
		status, code, message = http.StatusBadRequest, CodeInvalidJSONBody, err.Error()
	case errors.As(err, &fiberErr) && fiberErr.Code == http.StatusBadRequest:
		status, code, message = http.StatusBadRequest, CodeValidationFailed, fiberErr.Message
	case errors.As(err, &fiberErr) && fiberErr.Code == http.StatusNotFound:
		status, code, message = http.StatusNotFound, "RESOURCE_NOT_FOUND", "Rota não encontrada."
	case errors.As(err, &fiberErr) && fiberErr.Code == http.StatusMethodNotAllowed:
		status, code, message = http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Método não permitido para esta rota."
	default:
		slog.Error("rest: unhandled error", "error", err)
	}
	return c.Status(status).JSON(APIErrorResponse{Success: false, Error: APIErrorDetail{Code: code, Message: message}})
}
