# Third-party notices

PhonixOS is released under the [MIT License](LICENSE). The release binaries also contain the
third-party components listed below, each distributed under its own license. Full license texts
are available in each project's repository (and, for Go modules, in the module cache under
`$(go env GOMODCACHE)`).

## Go modules compiled into the binary

Generated with
`go list -deps -f '{{with .Module}}{{.Path}} {{.Version}}{{end}}' ./cmd/server`
for `GOOS=linux GOARCH=arm64`.

| Module | Version | License |
| :--- | :--- | :--- |
| [Go standard library](https://go.dev) | 1.23 | BSD-3-Clause |
| [github.com/andybalholm/brotli](https://github.com/andybalholm/brotli) | v1.1.0 | MIT |
| [github.com/dustin/go-humanize](https://github.com/dustin/go-humanize) | v1.0.1 | MIT |
| [github.com/fasthttp/websocket](https://github.com/fasthttp/websocket) | v1.5.8 | BSD-3-Clause |
| [github.com/gofiber/contrib/websocket](https://github.com/gofiber/contrib) | v1.3.4 | MIT |
| [github.com/gofiber/fiber/v2](https://github.com/gofiber/fiber) | v2.52.6 | MIT |
| [github.com/google/uuid](https://github.com/google/uuid) | v1.6.0 | BSD-3-Clause |
| [github.com/klauspost/compress](https://github.com/klauspost/compress) | v1.17.9 | BSD-3-Clause |
| [github.com/mattn/go-colorable](https://github.com/mattn/go-colorable) | v0.1.13 | MIT |
| [github.com/mattn/go-isatty](https://github.com/mattn/go-isatty) | v0.0.20 | MIT |
| [github.com/mattn/go-runewidth](https://github.com/mattn/go-runewidth) | v0.0.16 | MIT |
| [github.com/miekg/dns](https://github.com/miekg/dns) | v1.1.62 | BSD-3-Clause |
| [github.com/remyoudompheng/bigfft](https://github.com/remyoudompheng/bigfft) | v0.0.0-20230129092748-24d4a6f8daec | BSD-3-Clause |
| [github.com/rivo/uniseg](https://github.com/rivo/uniseg) | v0.2.0 | MIT |
| [github.com/savsgio/gotils](https://github.com/savsgio/gotils) | v0.0.0-20240303185622-093b76447511 | Apache-2.0 |
| [github.com/valyala/bytebufferpool](https://github.com/valyala/bytebufferpool) | v1.0.0 | MIT |
| [github.com/valyala/fasthttp](https://github.com/valyala/fasthttp) | v1.52.0 | MIT |
| [github.com/valyala/tcplisten](https://github.com/valyala/tcplisten) | v1.0.0 | MIT |
| [golang.org/x/net](https://cs.opensource.google/go/x/net) | v0.33.0 | BSD-3-Clause |
| [golang.org/x/sys](https://cs.opensource.google/go/x/sys) | v0.28.0 | BSD-3-Clause |
| [modernc.org/libc](https://gitlab.com/cznic/libc) | v1.55.3 | BSD-3-Clause |
| [modernc.org/mathutil](https://gitlab.com/cznic/mathutil) | v1.6.0 | BSD-3-Clause |
| [modernc.org/memory](https://gitlab.com/cznic/memory) | v1.8.0 | BSD-3-Clause |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) | v1.34.4 | BSD-3-Clause |

## Web interface (embedded static build)

| Component | License |
| :--- | :--- |
| [Svelte](https://github.com/sveltejs/svelte) runtime | MIT |
| [SvelteKit](https://github.com/sveltejs/kit) client runtime | MIT |
| [Tailwind CSS](https://github.com/tailwindlabs/tailwindcss) generated styles (including Preflight) | MIT |
| [IBM Plex Sans](https://github.com/IBM/plex) and [IBM Plex Mono](https://github.com/IBM/plex) fonts, packaged by [Fontsource](https://fontsource.org) | SIL Open Font License 1.1 |

The IBM Plex fonts are Copyright © 2017 IBM Corp., with Reserved Font Name "Plex", and are
licensed under the [SIL Open Font License, Version 1.1](https://openfontlicense.org). They may be
bundled with software under any license; the fonts themselves remain under the OFL.

Build-time tools (Vite, svelte-check, TypeScript, the Android SDK `d8` compiler) are not
distributed with PhonixOS.
