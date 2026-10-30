#!/usr/bin/env bash
# PhonixOS — compila o helper Android (internal/androidhelper/PhonixHelper.java) para dex (API 24+)
# no host/CI e o grava em internal/androidhelper/dex/, incorporado pelo go:embed. Executar
# antes do `go build` (docs/release.md). Requer JDK (javac 11+) e Android SDK com
# build-tools (d8) e uma plataforma android-* (android.jar). Nunca roda no dispositivo.
#
# Ambiente: ANDROID_HOME/ANDROID_SDK_ROOT (padrão $HOME/Android/Sdk),
#           ANDROID_PLATFORM (padrão: a maior instalada), ANDROID_BUILD_TOOLS (idem).
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd -P)
src="$root/internal/androidhelper/PhonixHelper.java"
out_dir="$root/internal/androidhelper/dex"
sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Android/Sdk}}"

fail() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ -f "$src" ] || fail "source not found: $src"
command -v javac >/dev/null 2>&1 || fail "javac not found (install a JDK 11+)"
[ -d "$sdk" ] || fail "Android SDK not found at $sdk (set ANDROID_HOME)"

platform="${ANDROID_PLATFORM:-$(find "$sdk/platforms" -mindepth 1 -maxdepth 1 -printf "%f\n" 2>/dev/null | sort -V | tail -n 1)}"
build_tools="${ANDROID_BUILD_TOOLS:-$(find "$sdk/build-tools" -mindepth 1 -maxdepth 1 -printf "%f\n" 2>/dev/null | sort -V | tail -n 1)}"
android_jar="$sdk/platforms/$platform/android.jar"
d8="$sdk/build-tools/$build_tools/d8"
[ -f "$android_jar" ] || fail "android.jar not found: $android_jar"
[ -x "$d8" ] || fail "d8 not found: $d8"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

javac --release 8 -nowarn -cp "$android_jar" -d "$work" "$src"
"$d8" --min-api 24 --lib "$android_jar" --output "$work" "$work"/*.class
mkdir -p "$out_dir"
cp "$work/classes.dex" "$out_dir/phonix-helper.dex"
echo "built $out_dir/phonix-helper.dex ($platform, build-tools $build_tools)"
