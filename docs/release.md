# Release e Build Externo

Contrato de build e distribuição do PhonixOS, consumido por `scripts/install.sh` e
`deploy/quickstart.sh`. Os valores canônicos ficam em [`scripts/release.conf`](../scripts/release.conf).

## Regras

- **Build estritamente externo.** O binário é compilado em CI/CD ou em máquina host
  (x86_64/arm64). Nunca compile Go nem rode Node/npm no Android/Termux: o compilador estoura a RAM
  ao processar `modernc.org/sqlite`.
- **Binário único e estático.** `CGO_ENABLED=0`, `GOOS=linux`; a SPA (SvelteKit + `adapter-static`)
  é incorporada via `//go:embed all:static` (`internal/api/static.go`). O dispositivo recebe só o
  binário: sem Node, sem `node_modules` e sem arquivos frontend avulsos.
- **Sem segredos no repositório.** A publicação usa a sessão do `gh` no host ou o `GITHUB_TOKEN` do CI.

## Versão e tag

| Item | Valor (1.0.0) |
| :--- | :--- |
| `PHONIX_VERSION` (`scripts/release.conf`) | `1.0.0` |
| Tag Git imutável do release | `v1.0.0` (`v${PHONIX_VERSION}`) |
| Versão em `GET /api/v1/health` | `"1.0.0"` (literal em `internal/api/handlers/system.go`) |
| `version` de `frontend/package.json` | `1.0.0` |

Para uma nova versão, atualize os quatro pontos no mesmo commit e crie o tag nesse commit. Um tag
publicado nunca é movido nem reaproveitado: correções geram nova versão.

## Matriz de artefatos

| `uname -m` no dispositivo | Variável | Asset | `GOARCH` | `GOARM` |
| :--- | :--- | :--- | :--- | :--- |
| `aarch64` | `PHONIX_BIN_ARM64` | `phonixos-linux-arm64` | `arm64` | — |
| `armv7l`, `armv8l` (userland 32 bits) | `PHONIX_BIN_ARM32` | `phonixos-linux-armv7` | `arm` | `7` |

Todos com `CGO_ENABLED=0` e `GOOS=linux`. O release também publica `SHA256SUMS` (formato
`sha256sum`) cobrindo os dois binários.

Download de um asset: `${PHONIX_RELEASE_BASE_URL}/<asset>`, isto é,
`https://github.com/gustavo-lopes-dev/PhonixOS/releases/download/v1.0.0/phonixos-linux-arm64`.

## Pipeline no host/CI (ordem obrigatória)

Pré-requisitos: Go conforme `go.mod` (1.23), Node 22 com npm, `git`, `curl`, `sha256sum` e
`gh` autenticado apenas para publicar. Execute a partir da raiz de um checkout limpo no tag:

```sh
. scripts/release.conf
git checkout "v${PHONIX_VERSION}"

# 1. Dependências do frontend (lockfile exato)
npm --prefix frontend ci

# 2. Validação e build estático do SvelteKit (gera frontend/build/)
npm --prefix frontend run check
npm --prefix frontend run build

# 3. Copiar a saída integral (inclui _app/, 200.html, index.html, service-worker.js)
find internal/api/static -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
cp -R frontend/build/. internal/api/static/
test -f internal/api/static/index.html && test -d internal/api/static/_app/immutable

# 4. Somente agora compilar o backend (go:embed lê internal/api/static/ no build)
go test ./...
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags="-s -w" -o "dist/${PHONIX_BIN_ARM64}" ./cmd/server
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  go build -trimpath -ldflags="-s -w" -o "dist/${PHONIX_BIN_ARM32}" ./cmd/server

# 5. Checksums
(cd dist && sha256sum "${PHONIX_BIN_ARM64}" "${PHONIX_BIN_ARM32}" > SHA256SUMS)
```

Inverter os passos 3 e 4 gera um binário sem a SPA (só `.gitkeep` incorporado). Em checkout montado
num contêiner com outro dono, acrescente `-buildvcs=false` ao `go build`.

## Conferência de cada binário

`file dist/phonixos-linux-*` deve indicar `ELF 64-bit ... ARM aarch64` e `ELF 32-bit ... ARM`,
ambos `statically linked`. Em seguida, execute **cada binário isolado** (no dispositivo, ou no host
via `qemu-user`/`docker run --platform linux/arm64|linux/arm/v7`), num diretório vazio, sem
`frontend/` nem `internal/api/static/` ao lado, com `APP_DATA_DIR` temporário:

```sh
BASE=http://127.0.0.1:8080
curl -fsS "$BASE/api/v1/health"                  # {"success":true,"data":{"status":"healthy",...,"version":"1.0.0"}}
curl -fsS "$BASE/" | grep -q '/_app/immutable/'  # index.html prerenderizado
asset=$(curl -fsS "$BASE/" | grep -o '/_app/immutable/[^"]*\.js' | head -n 1)
curl -fsS -o /dev/null -w '%{http_code} %{content_type}\n' "$BASE$asset"   # 200 text/javascript
curl -sS -o /dev/null -w '%{http_code}\n' --http1.1 --max-time 3 \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' "$BASE/ws/telemetry"   # 101
```

O `curl` do upgrade encerra por timeout após o `101`, que é o código impresso. A versão do health
deve ser igual a `PHONIX_VERSION`.

## Publicação no GitHub Releases

```sh
git push origin "v${PHONIX_VERSION}"
gh release create "v${PHONIX_VERSION}" --repo "${PHONIX_REPO}" --verify-tag \
  --title "PhonixOS v${PHONIX_VERSION}" \
  "dist/${PHONIX_BIN_ARM64}" "dist/${PHONIX_BIN_ARM32}" dist/SHA256SUMS
```

No CI, o mesmo fluxo roda num job disparado pelo push do tag `v*`, com `GH_TOKEN` vindo do
`GITHUB_TOKEN` do workflow (permissão `contents: write`), nunca de arquivo versionado.

## Consumo pelos scripts de instalação

- **`scripts/install.sh` (4.3):** carrega `scripts/release.conf` relativo ao próprio script, escolhe
  o asset pela matriz acima a partir de `uname -m`, baixa `${PHONIX_RELEASE_BASE_URL}/<asset>`
  (e pode conferir com `${PHONIX_RELEASE_BASE_URL}/SHA256SUMS`) e instala os templates
  `scripts/runit/` localizados junto a ele. Não compila nada.
- **`deploy/quickstart.sh` (5.2):** resolve um tag de release publicado e baixa o snapshot do código
  **desse mesmo tag imutável**, nunca de um branch:

  ```text
  https://github.com/gustavo-lopes-dev/PhonixOS/archive/refs/tags/v<versão>.tar.gz
  ```

  O arquivo extrai para `PhonixOS-<versão>/` e fornece `scripts/install.sh`, `scripts/release.conf`
  e `scripts/runit/{run,check,log/run,health/run}` da mesma versão. O quickstart confere a presença
  desses arquivos e que o tag é `v${PHONIX_VERSION}` antes de executar o instalador. O snapshot
  serve apenas para bootstrap: nunca é compilado no dispositivo, e o binário vem sempre do asset do
  release correspondente.
