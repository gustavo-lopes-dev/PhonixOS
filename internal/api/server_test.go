package api_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api/handlers"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api/ws"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/database"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/profile"
)

func TestRESTRoutes(t *testing.T) {
	db, mode, err := database.InitDB(filepath.Join(t.TempDir(), "phonix.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := database.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	store := database.NewStore(db)
	system := handlers.System{Profile: &profile.SystemProfile{Profile: profile.ProfileLite, StorageLockMode: mode}, Started: time.Now()}
	shortcuts := handlers.Shortcuts{Store: store}
	settings := handlers.Settings{Store: store}
	layout := handlers.Layout{Store: store}
	app := api.NewServer(api.Routes{
		Health: system.Health, Profile: system.GetProfile, Metrics: system.Metrics,
		ListShortcuts: shortcuts.List, CreateShortcut: shortcuts.Create,
		UpdateShortcut: shortcuts.Update, DeleteShortcut: shortcuts.Delete,
		GetSettings: settings.Get, PatchSettings: settings.Patch,
		GetLayout: layout.Get, PutLayout: layout.Put,
	}, nil)
	request := func(method, path, body string, status int, code string) map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var envelope map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status {
			t.Fatalf("%s %s: status %d, wanted %d: %v", method, path, resp.StatusCode, status, envelope)
		}
		if code == "" {
			if string(envelope["success"]) != "true" || len(envelope["data"]) == 0 {
				t.Fatalf("missing success data: %v", envelope)
			}
		} else {
			var detail api.APIErrorDetail
			if err := json.Unmarshal(envelope["error"], &detail); err != nil || detail.Code != code || string(envelope["success"]) != "false" {
				t.Fatalf("error code %q, wanted %q: %v (%v)", detail.Code, code, envelope, err)
			}
		}
		return envelope
	}
	request(http.MethodGet, "/api/v1/health", "", http.StatusOK, "")
	request(http.MethodGet, "/api/v1/system/profile", "", http.StatusOK, "")
	request(http.MethodGet, "/api/v1/settings", "", http.StatusOK, "")
	request(http.MethodGet, "/api/v1/layout", "", http.StatusOK, "")
	request(http.MethodGet, "/api/v1/shortcuts", "", http.StatusOK, "")
	request(http.MethodPost, "/api/v1/shortcuts", `{"title":`, http.StatusBadRequest, api.CodeInvalidJSONBody)
	request(http.MethodPost, "/api/v1/shortcuts", `{"title":"x","url":"bad"}`, http.StatusBadRequest, api.CodeValidationFailed)
	created := request(http.MethodPost, "/api/v1/shortcuts", `{"title":"Local","url":"http://192.168.1.2"}`, http.StatusCreated, "")
	var shortcut database.Shortcut
	if err := json.Unmarshal(created["data"], &shortcut); err != nil || shortcut.ID <= 0 {
		t.Fatalf("created shortcut: %+v, %v", shortcut, err)
	}
	path := fmt.Sprintf("/api/v1/shortcuts/%d", shortcut.ID)
	request(http.MethodPut, path, `{"is_pinned":true}`, http.StatusOK, "")
	request(http.MethodPut, path, `{"title":null}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, path, `{"is_pinned":null}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, path, `{"icon_url":"https://example.org/icon.png"}`, http.StatusOK, "")
	cleared := request(http.MethodPut, path, `{"icon_url":null}`, http.StatusOK, "")
	if string(cleared["data"]) == "" {
		t.Fatal("missing cleared shortcut")
	}
	var clearedShortcut database.Shortcut
	if err := json.Unmarshal(cleared["data"], &clearedShortcut); err != nil || clearedShortcut.IconURL != nil {
		t.Fatalf("null icon_url was not persisted: %+v (%v)", clearedShortcut, err)
	}
	request(http.MethodPut, "/api/v1/shortcuts/nope", `{}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodDelete, path, "", http.StatusOK, "")
	request(http.MethodDelete, path, "", http.StatusNotFound, api.CodeShortcutNotFound)
	request(http.MethodPatch, "/api/v1/settings", `{"theme_mode":"light"}`, http.StatusOK, "")
	request(http.MethodPatch, "/api/v1/settings", `{"theme_mode":null}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPatch, "/api/v1/settings", `{"custom_poll_interval_ms":200}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPatch, "/api/v1/settings", `{"custom_poll_interval_ms":1000}`, http.StatusOK, "")
	request(http.MethodPatch, "/api/v1/settings", `{"custom_poll_interval_ms":0}`, http.StatusOK, "")
	request(http.MethodPatch, "/api/v1/settings", `{"battery_saver_threshold":49}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"metrics_cpu","width":2,"height":2}]}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"metrics_cpu","width":2,"height":2,"is_visible":true}]}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"metrics_cpu","pos_x":0,"pos_y":0,"width":2,"height":2,"is_visible":null}]}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"metrics_cpu","pos_x":null,"width":2,"height":2,"is_visible":true}]}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"metrics_cpu","pos_x":0,"pos_y":0,"width":1,"height":2,"is_visible":true}]}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"metrics_cpu","pos_x":0,"pos_y":0,"width":2,"height":1,"is_visible":true}]}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"metrics_cpu","pos_x":5,"pos_y":0,"width":2,"height":2,"is_visible":true}]}`, http.StatusBadRequest, api.CodeValidationFailed)
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"metrics_cpu","pos_x":0,"pos_y":0,"width":2,"height":2,"is_visible":false}]}`, http.StatusOK, "")
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"metrics_cpu","pos_x":0,"pos_y":0,"width":2,"height":2,"is_visible":true}]}`, http.StatusOK, "")
	request(http.MethodPut, "/api/v1/layout", `{"cards":[{"card_key":"missing","pos_x":0,"pos_y":0,"width":2,"height":2,"is_visible":true}]}`, http.StatusNotFound, api.CodeCardNotFound)
	request(http.MethodGet, "/api/v1/missing", "", http.StatusNotFound, "RESOURCE_NOT_FOUND")

	// A rota síncrona usa o coletor real e o mesmo envelope REST.
	request(http.MethodGet, "/api/v1/system/metrics", "", http.StatusOK, "")
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/shortcuts", nil)
	req.Header.Set("Origin", "http://192.168.1.20:8080")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("CORS LAN: %v", resp.Header)
	}
}

func TestRecoverEnvelope(t *testing.T) {
	app := api.NewServer(api.Routes{Health: func(*fiber.Ctx) error { panic("test") }}, nil)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body api.APIErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusInternalServerError || body.Success || body.Error.Code != api.CodeInternalServerError {
		t.Fatalf("recover: %d %+v", resp.StatusCode, body)
	}
}

func TestWebSocketRequiresUpgrade(t *testing.T) {
	app := api.NewServer(api.Routes{}, ws.NewHub(&profile.SystemProfile{RecommendedPollIntervalS: 2}))
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/ws/telemetry", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body api.APIErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUpgradeRequired || body.Error.Code != api.CodeUpgradeRequired {
		t.Fatalf("upgrade response: %d %+v", resp.StatusCode, body)
	}
}

// Regressão (QA 2026-11, BUG-2): corpo acima do limite é 413 PAYLOAD_TOO_LARGE, nunca 500.
func TestPayloadTooLarge(t *testing.T) {
	called := false
	app := api.NewServer(api.Routes{CreateShortcut: func(c *fiber.Ctx) error {
		called = true
		return c.SendStatus(http.StatusCreated)
	}}, nil)
	// O limite é aplicado na leitura da conexão: app.Test não exercita esse caminho.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(listener) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	// Só o cabeçalho anuncia o tamanho: o servidor responde e fecha antes de ler o corpo,
	// como faz com clientes reais (curl recebeu este 413 no aparelho).
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(conn, "POST /api/v1/shortcuts HTTP/1.1\r\nHost: test\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n{", api.MaxBodyBytes+1)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var envelope api.APIErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if called || resp.StatusCode != http.StatusRequestEntityTooLarge || envelope.Success ||
		envelope.Error.Code != api.CodePayloadTooLarge {
		t.Fatalf("oversized body: called=%v %d %+v", called, resp.StatusCode, envelope)
	}
}
