#!/data/data/com.termux/files/usr/bin/sh
# PhonixOS — monitor de limite de recarga (Termux, espaço de usuário, sem root).
# Lê o nível de carga dos nós legíveis de /sys/class/power_supply/battery/ e, se
# indisponíveis, de `termux-battery-status` (Termux:API); obtém o limite em
# battery_saver_threshold de GET /api/v1/settings (padrão contratual 80) e alerta quando a
# carga atinge o limite com o carregador conectado, para que o usuário o desconecte.
# Nunca escreve em nós do kernel (charging_enabled e similares são protegidos): o valor
# de charging_enabled é apenas exibido quando legível. Falhas da API ou do sensor são
# informadas uma vez por mudança de estado e o monitoramento continua.
#
# Uso: battery_saver.sh [--once] [-h]
# Ambiente: PHONIX_API_URL, BATTERY_INTERVAL, BATTERY_SETTINGS_REFRESH, BATTERY_RESUME_LEVEL,
#           BATTERY_SYSFS_DIR, BATTERY_NOTIFY (auto|off).
# Saída de --once: 0 com leitura de bateria; 2 sem sensor disponível.

PREFIX="${PREFIX:-/data/data/com.termux/files/usr}"
PATH="$PREFIX/bin:$PATH"
export PREFIX PATH

PHONIX_API_URL="${PHONIX_API_URL:-http://127.0.0.1:8080}"
BATTERY_INTERVAL="${BATTERY_INTERVAL:-60}"                  # segundos entre leituras
BATTERY_SETTINGS_REFRESH="${BATTERY_SETTINGS_REFRESH:-300}" # segundos entre consultas à API
BATTERY_RESUME_LEVEL="${BATTERY_RESUME_LEVEL:-60}"          # lembrete para reconectar; 0 desativa
BATTERY_SYSFS_DIR="${BATTERY_SYSFS_DIR:-/sys/class/power_supply/battery}"
BATTERY_NOTIFY="${BATTERY_NOTIFY:-auto}"
DEFAULT_THRESHOLD=80

once=0
case "${1:-}" in
	'') ;;
	--once) once=1 ;;
	-h|--help)
		sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
		exit 0 ;;
	*) echo "usage: battery_saver.sh [--once] [-h]" >&2; exit 1 ;;
esac

log() { echo "$(date '+%Y-%m-%d %H:%M:%S') battery_saver: $*"; }

is_uint() {
	case "$1" in ''|*[!0-9]*) return 1 ;; esac
	return 0
}

# Executa um utilitário da Termux:API com limite de tempo: sem o app Termux:API instalado
# esses comandos podem ficar bloqueados indefinidamente.
with_timeout() {
	if command -v timeout >/dev/null 2>&1; then
		timeout 10 "$@"
	else
		"$@"
	fi
}

# Normaliza o status para charging|full|discharging|not_charging|unknown.
normalize_status() {
	case "$1" in
		[Cc]harging|CHARGING) echo charging ;;
		[Ff]ull|FULL) echo full ;;
		[Dd]ischarging|DISCHARGING) echo discharging ;;
		[Nn]ot\ charging|NOT_CHARGING) echo not_charging ;;
		*) echo unknown ;;
	esac
}

level="" status="" source="" charging_enabled=""

# Nível 1: SysFS (somente leitura).
read_sysfs() {
	[ -r "$BATTERY_SYSFS_DIR/capacity" ] || return 1
	value=$(cat "$BATTERY_SYSFS_DIR/capacity" 2>/dev/null) || return 1
	is_uint "$value" && [ "$value" -le 100 ] || return 1
	raw=""
	[ -r "$BATTERY_SYSFS_DIR/status" ] && raw=$(cat "$BATTERY_SYSFS_DIR/status" 2>/dev/null)
	level=$value
	status=$(normalize_status "$raw")
	source=sysfs
	charging_enabled=""
	if [ -r "$BATTERY_SYSFS_DIR/charging_enabled" ]; then
		charging_enabled=$(cat "$BATTERY_SYSFS_DIR/charging_enabled" 2>/dev/null)
	fi
	return 0
}

# Nível 2: Termux:API (`pkg install termux-api` + app Termux:API).
read_termux_api() {
	command -v termux-battery-status >/dev/null 2>&1 || return 1
	json=$(with_timeout termux-battery-status 2>/dev/null | tr -d '\n') || return 1
	value=$(printf '%s' "$json" | sed -n 's/.*"percentage"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p')
	raw=$(printf '%s' "$json" | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([A-Z_]*\)".*/\1/p')
	is_uint "$value" && [ "$value" -le 100 ] || return 1
	level=$value
	status=$(normalize_status "$raw")
	source=termux_api
	charging_enabled=""
	return 0
}

read_battery() {
	read_sysfs || read_termux_api
}

# Imprime battery_saver_threshold (50..100) do envelope de GET /api/v1/settings.
fetch_threshold() {
	body=$(curl --silent --fail --noproxy '*' --connect-timeout 3 --max-time 5 \
		"$PHONIX_API_URL/api/v1/settings" 2>/dev/null) || return 1
	case "$body" in *'"success":true'*) ;; *) return 1 ;; esac
	value=$(printf '%s' "$body" | sed -n 's/.*"battery_saver_threshold"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p')
	is_uint "$value" && [ "$value" -ge 50 ] && [ "$value" -le 100 ] || return 1
	echo "$value"
}

notify() {
	log "ALERT: $1"
	[ "$BATTERY_NOTIFY" = off ] && return 0
	command -v termux-notification >/dev/null 2>&1 || return 0
	with_timeout termux-notification --id phonixos-battery --title "PhonixOS" \
		--content "$1" >/dev/null 2>&1 || true
}

threshold=$DEFAULT_THRESHOLD
api_state=""      # ok | unavailable
sensor_state=""   # fonte atual ou none
last_fetch=-1
elapsed=0
high_alerted=0
low_alerted=0

refresh_threshold() {
	if value=$(fetch_threshold); then
		[ "$api_state" != ok ] || [ "$value" != "$threshold" ] &&
			log "threshold ${value}% (GET $PHONIX_API_URL/api/v1/settings)"
		[ "$value" != "$threshold" ] && high_alerted=0
		threshold=$value
		api_state=ok
	elif [ "$api_state" != unavailable ]; then
		log "PhonixOS API unavailable at $PHONIX_API_URL; using threshold ${threshold}% (last known or contract default) and retrying"
		api_state=unavailable
	fi
}

check_battery() {
	if ! read_battery; then
		if [ "$sensor_state" != none ]; then
			log "no readable battery sensor: $BATTERY_SYSFS_DIR/capacity is not readable and termux-battery-status is missing or failed (install the termux-api package and the Termux:API app); retrying"
			sensor_state=none
		fi
		return 1
	fi
	if [ "$sensor_state" != "$source" ]; then
		log "reading battery from $source"
		sensor_state=$source
	fi

	extra=""
	[ -n "$charging_enabled" ] && extra=" charging_enabled=$charging_enabled (read-only)"
	if [ "$once" -eq 1 ]; then
		log "level=${level}% status=$status threshold=${threshold}% source=$source$extra"
	fi

	case "$status" in
		charging|full) plugged=1 ;;
		*) plugged=0 ;;
	esac

	if [ "$plugged" -eq 1 ] && [ "$level" -ge "$threshold" ]; then
		if [ "$high_alerted" -eq 0 ]; then
			notify "Battery at ${level}% (limit ${threshold}%): unplug the charger to preserve the battery.$extra"
			high_alerted=1
		fi
	elif [ "$plugged" -eq 0 ] || [ "$level" -lt "$threshold" ]; then
		high_alerted=0
	fi

	if [ "$BATTERY_RESUME_LEVEL" -gt 0 ] && [ "$status" = discharging ] &&
		[ "$level" -le "$BATTERY_RESUME_LEVEL" ]; then
		if [ "$low_alerted" -eq 0 ]; then
			notify "Battery at ${level}%: reconnect the charger (resume level ${BATTERY_RESUME_LEVEL}%)."
			low_alerted=1
		fi
	elif [ "$status" != discharging ] || [ "$level" -gt "$BATTERY_RESUME_LEVEL" ]; then
		low_alerted=0
	fi
	return 0
}

if ! is_uint "$BATTERY_INTERVAL" || ! is_uint "$BATTERY_SETTINGS_REFRESH" ||
	! is_uint "$BATTERY_RESUME_LEVEL"; then
	echo "battery_saver: BATTERY_INTERVAL, BATTERY_SETTINGS_REFRESH and BATTERY_RESUME_LEVEL must be non-negative integers" >&2
	exit 1
fi
[ "$BATTERY_INTERVAL" -ge 1 ] || BATTERY_INTERVAL=1

if [ "$once" -eq 1 ]; then
	refresh_threshold
	check_battery || exit 2
	exit 0
fi

log "monitoring every ${BATTERY_INTERVAL}s (read-only; no charging node is ever written)"
while :; do
	if [ "$last_fetch" -lt 0 ] || [ $((elapsed - last_fetch)) -ge "$BATTERY_SETTINGS_REFRESH" ]; then
		refresh_threshold
		last_fetch=$elapsed
	fi
	check_battery
	sleep "$BATTERY_INTERVAL"
	elapsed=$((elapsed + BATTERY_INTERVAL))
done
