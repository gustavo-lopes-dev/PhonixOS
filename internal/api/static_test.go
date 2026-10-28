package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v2"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/api/ws"
	"github.com/gustavo-lopes-dev/PhonixOS/internal/profile"
)

func TestStaticAssetsAndPrecedence(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":                                 {Data: []byte("index")},
		"200.html":                                   {Data: []byte("fallback")},
		"service-worker.js":                          {Data: []byte("sw")},
		"manifest.webmanifest":                       {Data: []byte("{}")},
		"icons/icon-192.png":                         {Data: []byte("png")},
		"_app/version.json":                          {Data: []byte(`{"version":"1"}`)},
		"_app/immutable/entry/app.js":                {Data: []byte("app")},
		"_app/immutable/assets/plex-latin-400.woff2": {Data: []byte("font")},
	}
	health := func(c *fiber.Ctx) error { return c.SendString("health") }
	app := newServer(Routes{Health: health}, ws.NewHub(&profile.SystemProfile{RecommendedPollIntervalS: 2}), assets)

	for _, tc := range []struct {
		name, method, path, accept string
		status                     int
		body, contentType, cache   string
		code                       string
	}{
		{name: "root", method: http.MethodGet, path: "/", status: 200, body: "index", contentType: "text/html; charset=utf-8", cache: cacheRevalidate},
		{name: "head root", method: http.MethodHead, path: "/", status: 200, contentType: "text/html; charset=utf-8", cache: cacheRevalidate},
		{name: "bundle", method: http.MethodGet, path: "/_app/immutable/entry/app.js", status: 200, body: "app", contentType: "text/javascript; charset=utf-8", cache: cacheImmutable},
		{name: "font", method: http.MethodGet, path: "/_app/immutable/assets/plex-latin-400.woff2", status: 200, body: "font", contentType: "font/woff2", cache: cacheImmutable},
		{name: "version", method: http.MethodGet, path: "/_app/version.json", status: 200, contentType: "application/json", cache: cacheRevalidate},
		{name: "service worker", method: http.MethodGet, path: "/service-worker.js", status: 200, contentType: "text/javascript; charset=utf-8", cache: cacheRevalidate},
		{name: "manifest", method: http.MethodGet, path: "/manifest.webmanifest", status: 200, contentType: "application/manifest+json", cache: cacheRevalidate},
		{name: "icon", method: http.MethodGet, path: "/icons/icon-192.png", status: 200, contentType: "image/png", cache: cacheRevalidate},
		{name: "spa navigation", method: http.MethodGet, path: "/some/page", accept: "text/html,application/xhtml+xml", status: 200, body: "fallback", contentType: "text/html; charset=utf-8", cache: cacheRevalidate},
		{name: "non-navigation without file", method: http.MethodGet, path: "/some/page", accept: "application/json", status: 404, code: "RESOURCE_NOT_FOUND"},
		{name: "missing asset", method: http.MethodGet, path: "/_app/immutable/missing.js", accept: "text/html", status: 404, code: "RESOURCE_NOT_FOUND"},
		{name: "traversal", method: http.MethodGet, path: "/../index.html", status: 200, body: "index"},
		{name: "api route", method: http.MethodGet, path: "/api/v1/health", accept: "text/html", status: 200, body: "health"},
		{name: "unknown api", method: http.MethodGet, path: "/api/v1/missing", accept: "text/html", status: 404, code: "RESOURCE_NOT_FOUND"},
		{name: "api method", method: http.MethodPost, path: "/api/v1/health", status: 405, code: "METHOD_NOT_ALLOWED"},
		{name: "unknown ws", method: http.MethodGet, path: "/ws/missing", accept: "text/html", status: 426, code: CodeUpgradeRequired},
		{name: "ws without upgrade", method: http.MethodGet, path: "/ws/telemetry", status: 426, code: CodeUpgradeRequired},
		{name: "post page", method: http.MethodPost, path: "/", status: 404, code: "RESOURCE_NOT_FOUND"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.accept != "" {
				req.Header.Set("Accept", tc.accept)
			}
			res, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d (body %q)", res.StatusCode, tc.status, body)
			}
			if tc.body != "" && string(body) != tc.body {
				t.Errorf("body = %q, want %q", body, tc.body)
			}
			if tc.method == http.MethodHead && len(body) != 0 {
				t.Errorf("HEAD body = %q, want empty", body)
			}
			if tc.contentType != "" && res.Header.Get("Content-Type") != tc.contentType {
				t.Errorf("Content-Type = %q, want %q", res.Header.Get("Content-Type"), tc.contentType)
			}
			if tc.cache != "" && res.Header.Get("Cache-Control") != tc.cache {
				t.Errorf("Cache-Control = %q, want %q", res.Header.Get("Cache-Control"), tc.cache)
			}
			if tc.code != "" {
				var envelope APIErrorResponse
				if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error.Code != tc.code {
					t.Errorf("error envelope = %s (%v), want code %s", body, err, tc.code)
				}
			}
		})
	}
}

func TestEmbeddedStaticRootIsReadable(t *testing.T) {
	// O build pode estar ausente num checkout limpo (só .gitkeep); a raiz incorporada deve abrir.
	if _, err := staticAssets().Open("."); err != nil {
		t.Fatalf("open embedded static root: %v", err)
	}
}
