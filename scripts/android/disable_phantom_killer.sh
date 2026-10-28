#!/usr/bin/env bash
# PhonixOS — desativação opcional do Phantom Process Killer (Android 12+).
# Executado no COMPUTADOR HOST com ADB (platform-tools) e depuração USB/sem fio autorizada
# pelo usuário no aparelho; nunca roda dentro do Termux e não faz parte do runtime do servidor.
# Usa apenas `adb shell` (usuário shell do Android): sem su/sudo e sem pedir root.
# O servidor PhonixOS funciona sem esta otimização; ela apenas reduz o risco de o Android
# encerrar processos filhos do Termux (runsvdir, runsv, phonixos) em segundo plano.
#
# Uso: disable_phantom_killer.sh [-s SERIAL] [-y] [--check] [-h]
# Saída: 0 aplicado/já aplicado/não necessário; 1 erro de uso, ADB ou dispositivo;
#        2 cancelado pelo usuário; 3 permissão negada pelo dispositivo (limitação, não erro do servidor).

set -u

TARGET_VALUE=2147483647
ADB="${ADB:-adb}"
serial="${ANDROID_SERIAL:-}"
assume_yes=0
check_only=0

usage() {
	cat <<'EOF'
Usage: disable_phantom_killer.sh [-s SERIAL] [-y] [--check] [-h]

Optional host-side helper (run on your computer, not in Termux) that raises
activity_manager/max_phantom_processes to 2147483647 via ADB on Android 12+.
No root is used or requested. PhonixOS runs without this optimization.

  -s SERIAL   target device (default: $ANDROID_SERIAL or the only connected device)
  -y, --yes   apply without the confirmation prompt
  --check     only show the current state, change nothing
  -h, --help  show this help
EOF
}

say() { printf '%s\n' "$*"; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }

not_required_note() {
	say "PhonixOS does not require this optimization: the server keeps running under runit"
	say "without it, but Android may kill Termux background processes more aggressively."
}

while [ $# -gt 0 ]; do
	case "$1" in
		-s) [ $# -ge 2 ] || fail "-s requires a device serial"; serial="$2"; shift 2 ;;
		-s*) serial="${1#-s}"; shift ;;
		-y|--yes) assume_yes=1; shift ;;
		--check) check_only=1; shift ;;
		-h|--help) usage; exit 0 ;;
		*) usage >&2; exit 1 ;;
	esac
done

if ! command -v "$ADB" >/dev/null 2>&1; then
	say "adb not found. Install Android SDK Platform-Tools on this computer:"
	say "  https://developer.android.com/tools/releases/platform-tools"
	say "then enable Developer options > USB debugging (or Wireless debugging) on the device."
	not_required_note
	exit 1
fi

# Seleção do dispositivo: ANDROID_SERIAL/-s, ou o único listado em `adb devices`.
devices=$("$ADB" devices 2>/dev/null | sed -n '1d; s/^\([^[:space:]]\{1,\}\)[[:space:]]\{1,\}\([a-z]\{1,\}\).*/\1 \2/p')
if [ -z "$serial" ]; then
	count=$(printf '%s\n' "$devices" | grep -c .)
	if [ "$count" -eq 0 ]; then
		say "No device detected by adb. Connect the device and accept the debugging prompt,"
		say "or pair it with 'adb pair' / 'adb connect' for Wireless debugging."
		not_required_note
		exit 1
	fi
	if [ "$count" -gt 1 ]; then
		say "Multiple devices detected:"
		printf '%s\n' "$devices" | sed 's/^/  /'
		if [ -t 0 ]; then
			printf 'Serial of the target device: '
			read -r serial || exit 2
		fi
		[ -n "$serial" ] || fail "choose a device with -s SERIAL or ANDROID_SERIAL"
	else
		serial=${devices%% *}
	fi
fi

state=$(printf '%s\n' "$devices" | awk -v s="$serial" '$1 == s { print $2 }')
case "$state" in
	device) ;;
	unauthorized)
		say "Device $serial has not authorized this computer. Unlock it, accept the"
		say "'Allow USB debugging?' prompt and run this script again."
		exit 1 ;;
	"") fail "device $serial is not listed by 'adb devices'" ;;
	*) fail "device $serial is in state '$state' (expected 'device')" ;;
esac

# `adb shell` não repassa o exit code em versões antigas do adbd; a saída é sempre conferida.
ashell() { "$ADB" -s "$serial" shell "$@" 2>&1 | tr -d '\r'; }

sdk=$(ashell getprop ro.build.version.sdk)
case "$sdk" in
	''|*[!0-9]*) fail "could not read the Android SDK level from $serial (got: '$sdk')" ;;
esac
release=$(ashell getprop ro.build.version.release)
say "Device: $serial (Android $release, SDK $sdk)"

if [ "$sdk" -lt 31 ]; then
	say "Phantom Process Killer exists only on Android 12+ (SDK 31+). Nothing to do."
	exit 0
fi

current=$(ashell device_config get activity_manager max_phantom_processes)
say "Current activity_manager/max_phantom_processes: ${current:-<unknown>} (Android default: null = 32)"
if [ "$sdk" -ge 34 ]; then
	say "Android 14+: Developer options > 'Disable child process restrictions' has the same effect."
fi

if [ "$current" = "$TARGET_VALUE" ]; then
	say "Already applied. Nothing to do."
	exit 0
fi
[ "$check_only" -eq 1 ] && exit 0

if [ "$assume_yes" -ne 1 ]; then
	say ""
	say "This will run on the device (shell user, no root):"
	say "  device_config put activity_manager max_phantom_processes $TARGET_VALUE"
	if [ ! -t 0 ]; then
		say "Non-interactive session: re-run with -y to apply."
		exit 2
	fi
	printf 'Apply now? [y/N] '
	read -r answer || answer=""
	case "$answer" in
		y|Y|yes|YES|s|S|sim|SIM) ;;
		*) say "Cancelled. No change was made."; not_required_note; exit 2 ;;
	esac
fi

output=$(ashell device_config put activity_manager max_phantom_processes "$TARGET_VALUE")
applied=$(ashell device_config get activity_manager max_phantom_processes)

if [ "$applied" = "$TARGET_VALUE" ]; then
	say "Applied: max_phantom_processes=$applied"
	say "Notes: the value may be reset by system updates or by remote config sync; run this"
	say "script again (or use --check) if Termux processes start being killed again."
	say "To undo: adb shell device_config delete activity_manager max_phantom_processes"
	exit 0
fi

say "The device did not accept the change."
[ -n "$output" ] && say "  device output: $output"
case "$output" in
	*SecurityException*|*[Pp]ermission*|*[Nn]ot\ allowed*)
		say "This build does not allow the ADB shell user to write device_config. Writing it"
		say "would require root, which PhonixOS does not use or request."
		[ "$sdk" -ge 34 ] && say "Try Developer options > 'Disable child process restrictions' instead."
		not_required_note
		exit 3 ;;
esac
say "Read back: max_phantom_processes=${applied:-<unknown>}"
not_required_note
exit 1
