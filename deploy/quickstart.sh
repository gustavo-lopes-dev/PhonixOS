#!/data/data/com.termux/files/usr/bin/sh
# PhonixOS — quickstart público para Termux (sem root, sem checkout, sem build no aparelho).
#
#   curl -sSL https://raw.githubusercontent.com/gustavo-lopes-dev/PhonixOS/dev/deploy/quickstart.sh | bash
#   curl -sSL .../deploy/quickstart.sh | bash -s -- --version 1.0.0
#
# Resolve um tag de release publicado (o último, ou o pedido), baixa num diretório temporário
# o snapshot do código desse mesmo tag imutável (nunca de um branch), confere que ele traz
# scripts/install.sh, scripts/release.conf e os quatro templates scripts/runit/ e que
# PHONIX_VERSION corresponde ao tag, e então executa o scripts/install.sh do snapshot. O
# instalador baixa o binário pré-compilado do release correspondente (SHA256SUMS) e copia os
# serviços para $SVDIR; o diretório temporário é removido em sucesso ou falha.
#
# Uso: quickstart.sh [--version X.Y.Z] [-h]
# Ambiente: PHONIX_VERSION (equivale a --version), PHONIX_REPO (padrão gustavo-lopes-dev/PhonixOS),
#           PHONIX_GITHUB_URL (padrão https://github.com); repassados ao instalador: SVDIR,
#           PHONIX_START_TIMEOUT, PHONIX_INSTALL_BASE_URL.
# Saída: a do instalador (0 instalado; 1 pré-requisito; 2 download/checksum; 3 sem health) ou,
#        antes dele, 1 uso/pré-requisito/snapshot inválido e 2 release ou snapshot indisponível.

# Todo o corpo está em funções e só é executado na última linha: um download interrompido
# pelo pipe nunca executa um script pela metade.

say() { printf '%s\n' "$*"; }
step() { printf '==> %s\n' "$*"; }
fail() { printf 'error: %s\n' "$1" >&2; exit "${2:-1}"; }

usage() {
	say 'usage: quickstart.sh [--version X.Y.Z] [-h]'
	say '  curl -sSL <url>/deploy/quickstart.sh | bash -s -- --version X.Y.Z'
}

fetch() {
	curl --fail --location --silent --show-error --retry 3 --connect-timeout 15 "$@"
}

# Tag do release mais recente publicado: o GitHub redireciona /releases/latest para
# /releases/tag/<tag> (sem a cota da API REST); a API fica como alternativa.
latest_tag() {
	effective=$(fetch --head --output /dev/null --write-out '%{url_effective}' \
		"$GITHUB_URL/$REPO/releases/latest" 2>/dev/null) || effective=''
	case "$effective" in
		*/releases/tag/*) printf '%s\n' "${effective##*/releases/tag/}"; return 0 ;;
	esac
	fetch --header 'Accept: application/vnd.github+json' \
		"https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null |
		sed -n 's/^ *"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1
}

# shellcheck disable=SC2329 # chamada pelo trap EXIT
cleanup() {
	[ -n "${tmp_dir:-}" ] && rm -rf "$tmp_dir"
	tmp_dir=''
}

main() {
	PREFIX="${PREFIX:-/data/data/com.termux/files/usr}"
	HOME="${HOME:-/data/data/com.termux/files/home}"
	PATH="$PREFIX/bin:$PATH"
	export PREFIX HOME PATH

	REPO="${PHONIX_REPO:-gustavo-lopes-dev/PhonixOS}"
	GITHUB_URL="${PHONIX_GITHUB_URL:-https://github.com}"
	GITHUB_URL="${GITHUB_URL%/}"
	requested="${PHONIX_VERSION:-}"

	while [ "$#" -gt 0 ]; do
		case "$1" in
			-h|--help) usage; exit 0 ;;
			-v|--version)
				[ "$#" -ge 2 ] || { usage >&2; exit 1; }
				requested="$2"; shift 2 ;;
			--version=*) requested="${1#--version=}"; shift ;;
			*) usage >&2; exit 1 ;;
		esac
	done
	requested="${requested#v}"

	# --- Pré-requisitos (Termux, usuário comum, curl/tar/gzip) ----------------------------

	[ "$(id -u)" != 0 ] || fail "do not run as root: PhonixOS runs as the Termux user (zero root)"
	[ -d "$PREFIX" ] || fail "Termux prefix not found: $PREFIX (this quickstart targets Termux on Android)"

	# comando:pacote Termux que o fornece (gzip: descompressão do tar.gz)
	need_pkgs=''
	for pair in curl:curl tar:tar gzip:gzip mktemp:coreutils; do
		command -v "${pair%%:*}" >/dev/null 2>&1 && continue
		case " $need_pkgs " in *" ${pair#*:} "*) ;; *) need_pkgs="$need_pkgs ${pair#*:}" ;; esac
	done
	if [ -n "$need_pkgs" ]; then
		command -v pkg >/dev/null 2>&1 || fail "missing packages:$need_pkgs (install them with pkg)"
		step "Installing Termux packages:$need_pkgs"
		# shellcheck disable=SC2086 # lista de pacotes separada por espaço
		pkg install -y $need_pkgs || fail "pkg install failed:$need_pkgs"
		for c in curl tar gzip mktemp; do
			command -v "$c" >/dev/null 2>&1 || fail "command still unavailable after pkg install: $c"
		done
	fi

	# --- Tag do release publicado ---------------------------------------------------------

	if [ -n "$requested" ]; then
		tag="v$requested"
	else
		step "Resolving the latest PhonixOS release ($REPO)"
		tag=$(latest_tag)
		[ -n "$tag" ] || fail "cannot resolve the latest published release of $REPO" 2
	fi
	printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' ||
		fail "invalid release tag: $tag (expected vX.Y.Z)"
	version="${tag#v}"

	# Um release publicado tem o SHA256SUMS anexado; um tag sem release (ou rascunho) não.
	fetch --output /dev/null "$GITHUB_URL/$REPO/releases/download/$tag/SHA256SUMS" 2>/dev/null ||
		fail "release $tag of $REPO is not published (no SHA256SUMS asset)" 2
	say "    release $tag"

	# --- Snapshot do mesmo tag em diretório temporário ------------------------------------

	tmp_dir=''
	trap cleanup EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM
	tmp_base="${TMPDIR:-$PREFIX/tmp}"
	[ -d "$tmp_base" ] && [ -w "$tmp_base" ] || tmp_base="$HOME"
	tmp_dir=$(mktemp -d "$tmp_base/phonixos-quickstart.XXXXXX") ||
		fail "cannot create a temporary directory in $tmp_base"

	archive_url="$GITHUB_URL/$REPO/archive/refs/tags/$tag.tar.gz"
	step "Downloading the $tag snapshot ($archive_url)"
	fetch --output "$tmp_dir/snapshot.tar.gz" "$archive_url" ||
		fail "download failed: $archive_url" 2
	{ mkdir "$tmp_dir/src" && tar -xzf "$tmp_dir/snapshot.tar.gz" -C "$tmp_dir/src"; } ||
		fail "cannot extract $archive_url" 2

	# O arquivo do GitHub extrai para um único diretório (PhonixOS-<versão>/).
	set -- "$tmp_dir"/src/*
	[ "$#" -eq 1 ] && [ -d "$1" ] || fail "unexpected snapshot layout in $archive_url"
	snapshot="$1"

	missing=''
	for f in scripts/install.sh scripts/release.conf scripts/runit/run scripts/runit/check \
		scripts/runit/log/run scripts/runit/health/run; do
		[ -f "$snapshot/$f" ] && [ -r "$snapshot/$f" ] || missing="$missing $f"
	done
	[ -z "$missing" ] || fail "the $tag snapshot is missing required files:$missing"

	# release.conf lido num subshell: só as variáveis conferidas saem dele.
	# shellcheck disable=SC1091 # arquivo do snapshot baixado
	conf=$(. "$snapshot/scripts/release.conf" && printf '%s\n%s\n' "${PHONIX_VERSION:-}" "${PHONIX_REPO:-}") ||
		fail "cannot read scripts/release.conf of the $tag snapshot"
	conf_version=$(printf '%s\n' "$conf" | sed -n 1p)
	conf_repo=$(printf '%s\n' "$conf" | sed -n 2p)
	[ "$conf_version" = "$version" ] ||
		fail "tag $tag does not match PHONIX_VERSION=\"$conf_version\" in its scripts/release.conf"
	[ "$conf_repo" = "$REPO" ] ||
		fail "the $tag snapshot downloads binaries from \"$conf_repo\", not from $REPO"
	say "    snapshot ok: installer, release.conf ($version) and runit templates"

	# --- Instalador do snapshot -----------------------------------------------------------

	# Executado como arquivo (localiza release.conf e runit/ relativamente a si) e a partir de
	# $HOME; copia os templates para $SVDIR, então o snapshot pode ser removido depois.
	step "Running scripts/install.sh from $tag"
	cd "$HOME" || fail "cannot change to $HOME"
	sh "$snapshot/scripts/install.sh"
	status=$?

	if [ "$status" -eq 0 ] || [ "$status" -eq 3 ]; then
		say ''
		say "Remote access outside home (Tailscale/WireGuard, no root):"
		say "  $GITHUB_URL/$REPO/blob/$tag/docs/network/remote_access.md"
	fi
	exit "$status"
}

main "$@"
