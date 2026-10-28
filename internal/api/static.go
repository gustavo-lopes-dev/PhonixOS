package api

import (
	"embed"
	"errors"
	"io/fs"
	"mime"
	"path"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// staticFiles contém o build estático do frontend (SvelteKit + adapter-static), copiado de
// frontend/build/ para internal/api/static/ no host/CI antes do `go build`. O prefixo `all:`
// inclui diretórios iniciados por `_` (como `_app/`) e arquivos ocultos (`.gitkeep`).
//
//go:embed all:static
var staticFiles embed.FS

const (
	staticIndex    = "index.html"
	staticFallback = "200.html"

	cacheImmutable  = "public, max-age=31536000, immutable"
	cacheRevalidate = "no-cache"
)

// staticContentTypes cobre os assets gerados pelo build sem depender de /etc/mime.types,
// ausente no Termux; o restante recorre à tabela embutida do Go.
var staticContentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".ico":         "image/x-icon",
	".woff2":       "font/woff2",
	".woff":        "font/woff",
	".txt":         "text/plain; charset=utf-8",
}

// staticAssets devolve o build incorporado com a raiz em static/.
func staticAssets() fs.FS {
	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err) // "static" é um caminho literal válido; fs.Sub só falha com caminho inválido.
	}
	return assets
}

// staticHandler serve a SPA para GET/HEAD fora de /api/ e /ws/: arquivo existente, senão o
// fallback 200.html apenas para navegações de página. Asset ausente segue para o 404 padrão.
func staticHandler(assets fs.FS) fiber.Handler {
	return func(c *fiber.Ctx) error {
		method := c.Method()
		if (method != fiber.MethodGet && method != fiber.MethodHead) || isAPIPath(c.Path()) {
			return c.Next()
		}
		name, ok := resolveStatic(assets, c.Path())
		if !ok {
			if !isNavigation(c) {
				return c.Next()
			}
			name = staticFallback
		}
		return sendStatic(c, assets, name)
	}
}

// isAPIPath reserva os prefixos da API e do WebSocket para as rotas originais.
func isAPIPath(requestPath string) bool {
	for _, prefix := range []string{"/api", "/ws"} {
		if requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/") {
			return true
		}
	}
	return false
}

// resolveStatic mapeia o caminho da URL para um arquivo regular: exato, `<dir>/index.html` ou
// `<página>.html` (páginas prerenderizadas pelo adapter-static).
func resolveStatic(assets fs.FS, requestPath string) (string, bool) {
	clean := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	candidates := []string{clean, path.Join(clean, staticIndex)}
	if clean == "" {
		candidates = []string{staticIndex}
	} else if path.Ext(clean) == "" {
		candidates = append(candidates, clean+".html")
	}
	for _, name := range candidates {
		if !fs.ValidPath(name) {
			continue
		}
		info, err := fs.Stat(assets, name)
		if err == nil && info.Mode().IsRegular() {
			return name, true
		}
	}
	return "", false
}

// isNavigation identifica uma navegação de página: sem extensão no último segmento e pedida
// como documento HTML pelo navegador.
func isNavigation(c *fiber.Ctx) bool {
	if path.Ext(c.Path()) != "" {
		return false
	}
	return c.Get("Sec-Fetch-Mode") == "navigate" || strings.Contains(c.Get(fiber.HeaderAccept), "text/html")
}

func sendStatic(c *fiber.Ctx, assets fs.FS, name string) error {
	file, err := assets.Open(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fiber.ErrNotFound
		}
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return errors.Join(err, file.Close())
	}
	c.Set(fiber.HeaderContentType, contentTypeFor(name))
	c.Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(name, "_app/immutable/") {
		c.Set(fiber.HeaderCacheControl, cacheImmutable)
	} else {
		// HTML, service-worker.js, _app/version.json, manifesto e ícones: sempre revalidar.
		c.Set(fiber.HeaderCacheControl, cacheRevalidate)
	}
	c.Status(fiber.StatusOK)
	// O fasthttp fecha o arquivo ao terminar o envio (io.Closer) e omite o corpo em HEAD.
	c.Context().SetBodyStream(file, int(info.Size()))
	return nil
}

func contentTypeFor(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if contentType, ok := staticContentTypes[ext]; ok {
		return contentType
	}
	if contentType := mime.TypeByExtension(ext); contentType != "" {
		return contentType
	}
	return fiber.MIMEOctetStream
}
