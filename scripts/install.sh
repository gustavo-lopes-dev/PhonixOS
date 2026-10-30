#!/data/data/com.termux/files/usr/bin/sh
# PhonixOS — instalador universal para Termux (sem root, build estritamente externo).
# Detecta a arquitetura por `uname -m`, baixa o binário pré-compilado do GitHub Releases
# (frontend já incorporado) conferindo-o com SHA256SUMS, cria $HOME/.phonixos/bin e
# $HOME/data e instala/registra no termux-services o serviço principal "phonixos"
# (run, check, log/run) e o auxiliar "phonixos-health" (health/run). Nunca compila nada
# nem instala Node; scripts/release.conf e scripts/runit/ são lidos relativamente a este
# arquivo (checkout local ou snapshot do mesmo tag preparado por deploy/quickstart.sh) e
# copiados para o diretório persistente dos serviços.
#
# Uso: sh scripts/install.sh [-h]
# Ambiente: SVDIR (padrão $PREFIX/var/service), PHONIX_START_TIMEOUT (segundos, padrão 45),
#           PHONIX_INSTALL_BASE_URL (espelho dos assets; padrão PHONIX_RELEASE_BASE_URL).
# Saída: 0 instalado; 1 uso/pré-requisito/arquivos ausentes/arquitetura; 2 download ou
#        checksum; 3 instalado, mas o serviço não respondeu ao health check.

PREFIX="${PREFIX:-/data/data/com.termux/files/usr}"
HOME="${HOME:-/data/data/com.termux/files/home}"
PATH="$PREFIX/bin:$PATH"
SVDIR="${SVDIR:-$PREFIX/var/service}"
export PREFIX HOME PATH SVDIR

PHONIX_START_TIMEOUT="${PHONIX_START_TIMEOUT:-45}"
PHONIX_HOME="$HOME/.phonixos"
BIN_DIR="$PHONIX_HOME/bin"
BIN_PATH="$BIN_DIR/phonixos"   # caminho fixo lido por scripts/runit/run
DATA_DIR="$HOME/data"          # $APP_DATA_DIR -> $HOME/data -> ./data (internal/database)
SERVICE=phonixos
HEALTH_SERVICE=phonixos-health
LOG_DIR="$PREFIX/var/log/sv/$SERVICE"

case "${1:-}" in
	'') ;;
	-h|--help)
		sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
		exit 0 ;;
	*) echo "usage: install.sh [-h]" >&2; exit 1 ;;
esac

say() { printf '%s\n' "$*"; }
step() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
fail() { printf 'error: %s\n' "$*" >&2; exit "${2:-1}"; }

tmp_dir=''
# shellcheck disable=SC2329 # chamada pelo trap EXIT
cleanup() { [ -n "$tmp_dir" ] && rm -rf "$tmp_dir"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

sha256_of() { sha256sum "$1" 2>/dev/null | cut -d ' ' -f 1; }

# --- Fontes do instalador (relativas a este arquivo, nunca ao diretório corrente) ----------

case "$0" in
	*/*) script_dir=$(cd "${0%/*}" && pwd -P) ;;
	*) script_dir=$(pwd -P) ;;
esac
if [ -z "$script_dir" ] || [ ! -f "$script_dir/install.sh" ]; then
	fail "cannot locate the installer directory; run it from a file (sh scripts/install.sh), not from a pipe"
fi

RELEASE_CONF="$script_dir/release.conf"
RUNIT_DIR="$script_dir/runit"
missing=''
for f in "$RELEASE_CONF" "$RUNIT_DIR/run" "$RUNIT_DIR/check" "$RUNIT_DIR/log/run" "$RUNIT_DIR/health/run"; do
	[ -f "$f" ] && [ -r "$f" ] || missing="$missing
  $f"
done
[ -z "$missing" ] || fail "required installer files are missing:$missing
Use a full checkout or the release snapshot of the same tag."

# shellcheck source-path=SCRIPTDIR source=release.conf
. "$RELEASE_CONF"
for v in PHONIX_REPO PHONIX_VERSION PHONIX_RELEASE_BASE_URL PHONIX_BIN_ARM64 PHONIX_BIN_ARM32; do
	eval "val=\${$v:-}"
	[ -n "$val" ] || fail "$RELEASE_CONF does not define $v"
done
BASE_URL="${PHONIX_INSTALL_BASE_URL:-$PHONIX_RELEASE_BASE_URL}"

# O `sv` abre o diretório corrente e falha se ele não for legível (no Android, `/` não é):
# trabalha sempre a partir de $HOME, como scripts/runit/run.
cd "$HOME" || fail "cannot change to $HOME"

# --- Pré-requisitos -------------------------------------------------------------------------

[ "$(id -u)" != 0 ] || fail "do not run as root: PhonixOS runs as the Termux user (zero root)"
[ -d "$PREFIX" ] || fail "Termux prefix not found: $PREFIX (this installer targets Termux on Android)"

machine=$(uname -m)
case "$machine" in
	aarch64|arm64) arch=arm64; asset="$PHONIX_BIN_ARM64" ;;
	armv7l|armv8l) arch=armv7; asset="$PHONIX_BIN_ARM32" ;;
	*) fail "unsupported architecture: $machine (supported: aarch64, armv7l/armv8l)" ;;
esac

# comando:pacote Termux que o fornece
need_pkgs=''
for pair in curl:curl sha256sum:coreutils mktemp:coreutils sv:termux-services \
	sv-enable:termux-services runsv:termux-services svlogd:termux-services; do
	command -v "${pair%%:*}" >/dev/null 2>&1 && continue
	case " $need_pkgs " in *" ${pair#*:} "*) ;; *) need_pkgs="$need_pkgs ${pair#*:}" ;; esac
done
if [ -n "$need_pkgs" ]; then
	command -v pkg >/dev/null 2>&1 || fail "missing packages:$need_pkgs (install them with pkg)"
	step "Installing Termux packages:$need_pkgs"
	# shellcheck disable=SC2086 # lista de pacotes separada por espaço
	pkg install -y $need_pkgs || fail "pkg install failed:$need_pkgs"
	for c in curl sha256sum mktemp sv sv-enable runsv svlogd; do
		command -v "$c" >/dev/null 2>&1 || fail "command still unavailable after pkg install: $c"
	done
fi

say "PhonixOS v$PHONIX_VERSION installer ($machine -> $arch)"

# --- Diretórios -----------------------------------------------------------------------------

step "Creating $BIN_DIR and $DATA_DIR"
mkdir -p "$BIN_DIR" "$DATA_DIR" "$SVDIR" || fail "cannot create installation directories"
if [ -n "${APP_DATA_DIR:-}" ] && [ "$APP_DATA_DIR" != "$DATA_DIR" ]; then
	warn "APP_DATA_DIR=$APP_DATA_DIR is set in this shell; the service uses it only if it is also in the environment of the termux-services daemon, otherwise the database stays in $DATA_DIR"
fi
# Mesmo sistema de arquivos do destino: a troca do binário é um rename atômico.
tmp_dir=$(mktemp -d "$PHONIX_HOME/.install.XXXXXX") || fail "cannot create a temporary directory in $PHONIX_HOME"

# --- Binário do release ---------------------------------------------------------------------

download() {
	curl --fail --location --silent --show-error --retry 3 --connect-timeout 15 \
		--output "$2" "$1"
}

step "Downloading $asset ($BASE_URL)"
download "$BASE_URL/$asset" "$tmp_dir/$asset" || fail "download failed: $BASE_URL/$asset" 2
download "$BASE_URL/SHA256SUMS" "$tmp_dir/SHA256SUMS" || fail "download failed: $BASE_URL/SHA256SUMS" 2

expected=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$tmp_dir/SHA256SUMS")
[ -n "$expected" ] || fail "SHA256SUMS has no entry for $asset" 2
actual=$(sha256_of "$tmp_dir/$asset")
[ "$expected" = "$actual" ] || fail "checksum mismatch for $asset (expected $expected, got $actual)" 2
say "    sha256 ok: $actual"

binary_changed=0
if [ -f "$BIN_PATH" ] && [ "$(sha256_of "$BIN_PATH")" = "$actual" ] && [ -x "$BIN_PATH" ]; then
	say "    $BIN_PATH is already this version"
else
	{ chmod 755 "$tmp_dir/$asset" && mv -f "$tmp_dir/$asset" "$BIN_PATH"; } ||
		fail "cannot install the binary at $BIN_PATH"
	binary_changed=1
	say "    installed $BIN_PATH"
fi

# --- Serviços runit (termux-services) -------------------------------------------------------

# Copia via arquivo temporário + rename (o runsv nunca lê um script pela metade) e
# informa por status 0 quando o conteúdo mudou.
install_file() {
	if [ -f "$2" ] && [ "$(sha256_of "$1")" = "$(sha256_of "$2")" ]; then
		chmod 755 "$2" || fail "cannot chmod $2"
		return 1
	fi
	{ cp "$1" "$2.new" && chmod 755 "$2.new" && mv -f "$2.new" "$2"; } || fail "cannot install $2"
	return 0
}

# Serviço novo é montado num diretório oculto (ignorado pelo runsvdir) com "down" e só então
# renomeado para $SVDIR: o runsv o encontra completo (inclusive log/, detectado apenas na sua
# partida) e não o inicia antes do sv-enable. Serviço existente é atualizado arquivo a arquivo.
service_target() {
	if [ -f "$SVDIR/$1/run" ]; then
		printf '%s\n' "$SVDIR/$1"
		return 0
	fi
	stage="$SVDIR/.$1.new"
	{ rm -rf "$stage" && mkdir -p "$stage" && : >"$stage/down"; } || fail "cannot create $stage"
	printf '%s\n' "$stage"
}

publish_service() {
	[ "$1" = "$SVDIR/$2" ] && return 0
	{ rm -rf "${SVDIR:?}/$2" && mv "$1" "$SVDIR/$2"; } || fail "cannot create service directory $SVDIR/$2"
}

step "Installing services in $SVDIR"
main_dir="$SVDIR/$SERVICE"
health_dir="$SVDIR/$HEALTH_SERVICE"
main_target=$(service_target "$SERVICE") || exit 1
health_target=$(service_target "$HEALTH_SERVICE") || exit 1
mkdir -p "$main_target/log" || fail "cannot create $main_target/log"

main_changed=$binary_changed
log_changed=0
health_changed=0
install_file "$RUNIT_DIR/run" "$main_target/run" && main_changed=1
install_file "$RUNIT_DIR/check" "$main_target/check" && main_changed=1
install_file "$RUNIT_DIR/log/run" "$main_target/log/run" && log_changed=1
install_file "$RUNIT_DIR/health/run" "$health_target/run" && health_changed=1
publish_service "$main_target" "$SERVICE"
publish_service "$health_target" "$HEALTH_SERVICE"
say "    $SERVICE: run, check, log/run"
say "    $HEALTH_SERVICE: run"

# Espera o runsv do serviço abrir supervise/ok (o runsvdir varre $SVDIR a cada 5 s).
wait_supervised() {
	n=0
	while [ ! -p "$SVDIR/$1/supervise/ok" ]; do
		n=$((n + 1))
		[ "$n" -gt "$2" ] && return 1
		sleep 1
	done
	return 0
}

is_running() { sv status "$1" 2>/dev/null | grep -q '^run:'; }

step "Registering services with termux-services"
supervised=1
if ! wait_supervised "$SERVICE" 8; then
	if command -v service-daemon >/dev/null 2>&1; then
		say "    starting the termux-services daemon"
		service-daemon start >/dev/null 2>&1
	fi
	wait_supervised "$SERVICE" 12 || supervised=0
fi

if [ "$supervised" = 1 ]; then
	was_running=0
	is_running "$SERVICE" && was_running=1
	sv-enable "$SERVICE" >/dev/null || fail "sv-enable $SERVICE failed"
	if [ "$was_running" = 1 ] && [ "$main_changed" = 1 ]; then
		say "    restarting $SERVICE with the new files"
		sv -w "$PHONIX_START_TIMEOUT" restart "$SERVICE" >/dev/null 2>&1 ||
			warn "sv restart $SERVICE did not report a healthy restart"
	fi
	if [ "$log_changed" = 1 ] && is_running "$SERVICE/log"; then
		sv restart "$SERVICE/log" >/dev/null 2>&1 || warn "sv restart $SERVICE/log reported a failure"
	fi

	if wait_supervised "$HEALTH_SERVICE" 12; then
		was_running=0
		is_running "$HEALTH_SERVICE" && was_running=1
		sv-enable "$HEALTH_SERVICE" >/dev/null || fail "sv-enable $HEALTH_SERVICE failed"
		if [ "$was_running" = 1 ] && [ "$health_changed" = 1 ]; then
			sv restart "$HEALTH_SERVICE" >/dev/null 2>&1 || warn "sv restart $HEALTH_SERVICE reported a failure"
		fi
	else
		rm -f "$health_dir/down"
		warn "$HEALTH_SERVICE is not supervised yet; it starts with the termux-services daemon"
	fi
else
	# Sem daemon ativo: deixa ambos habilitados (sem "down") para subirem com ele.
	rm -f "$main_dir/down" "$health_dir/down"
fi

# --- Verificação e endereços ----------------------------------------------------------------

# IPv4 literais das interfaces ativas, como network.LocalIPv4 do servidor: sem loopback,
# link-local ou não roteáveis. Fallback: URLs que o próprio servidor registrou no log.
local_ipv4() {
	{
		if command -v ip >/dev/null 2>&1; then
			ip -4 -o addr show up 2>/dev/null | awk '{ print $4 }'
		fi
		if command -v ifconfig >/dev/null 2>&1; then
			ifconfig 2>/dev/null | sed -n 's/.*inet \(addr:\)\{0,1\}\([0-9.]*\).*/\2/p'
		fi
	} | cut -d / -f 1 | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' |
		grep -v -E '^(127|0|169\.254|22[4-9]|2[3-5][0-9])\.' | sort -u
}

server_logged_urls() {
	[ -r "$LOG_DIR/current" ] || return 0
	tail -n 200 "$LOG_DIR/current" | sed -n 's/.*url=\(http:\/\/[0-9.]*:8080\).*/\1/p' | sort -u
}

healthy=0
if [ "$supervised" = 1 ]; then
	step "Waiting for $SERVICE to answer /api/v1/health (up to ${PHONIX_START_TIMEOUT}s)"
	n=0
	while [ "$n" -lt "$PHONIX_START_TIMEOUT" ]; do
		if "$main_dir/check"; then
			healthy=1
			break
		fi
		n=$((n + 1))
		sleep 1
	done
	if [ "$healthy" = 1 ]; then
		version=$(curl --silent --noproxy '*' --max-time 3 http://127.0.0.1:8080/api/v1/health 2>/dev/null |
			sed -n 's/.*"version":"\([^"]*\)".*/\1/p')
		say "    healthy (version ${version:-unknown})"
		[ "$version" = "$PHONIX_VERSION" ] ||
			warn "the running server reports version '${version:-unknown}', expected $PHONIX_VERSION"
	fi
fi

say ''
if [ "$supervised" = 0 ]; then
	say "PhonixOS v$PHONIX_VERSION is installed, but the termux-services daemon is not running."
	say "Close and reopen Termux (or run: service-daemon start); both services are enabled and"
	say "start with it. Then check: sv status $SERVICE"
elif [ "$healthy" = 0 ]; then
	say "PhonixOS v$PHONIX_VERSION is installed and enabled, but it did not answer the health check."
	say "Inspect: sv status $SERVICE ; tail -n 50 $LOG_DIR/current"
	exit 3
else
	say "PhonixOS v$PHONIX_VERSION is running."
fi

urls=$(local_ipv4 | sed 's/.*/http:\/\/&:8080/')
[ -n "$urls" ] || urls=$(server_logged_urls)
say ''
say 'Dashboard (online access over the LAN, by literal IP):'
if [ -n "$urls" ]; then
	printf '%s\n' "$urls" | sed 's/^/  /'
else
	say '  (no LAN IPv4 address detected; connect to Wi-Fi and use http://<device-ip>:8080)'
fi
say '  http://localhost:8080 on this device'
say ''
say 'Notes:'
say '  - http://phonix.local:8080 is not announced: mDNS is not enabled in this version. Use the IP.'
say '  - Over plain HTTP, other LAN devices get the dashboard online only. PWA installation and'
say '    offline cache require trusted HTTPS (reverse proxy): see docs/network/https_pwa.md.'
say '  - Keep Termux alive with termux-wake-lock. Optional Android helpers (Phantom Process Killer'
say '    via ADB, charge-limit monitor) are in scripts/android/ of the repository.'
say ''
say "Binary: $BIN_PATH   Data: $DATA_DIR   Logs: $LOG_DIR/current"
say "Services: sv status $SERVICE $HEALTH_SERVICE   (sv-disable $SERVICE to stop at boot)"
exit 0
