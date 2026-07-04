#!/bin/sh
set -eu

usage() {
	cat >&2 <<'EOF'
usage: scripts/live/restore-caddy.sh <user@host> </root/slipway-backups/timestamp> [--print-commands]

Stops the Slipway Dockerized Caddy container, restores /etc/caddy when present
in the backup, reloads systemd, and restarts the system Caddy service when it
exists.

--print-commands prints the SSH commands without executing them.
EOF
}

if [ $# -lt 2 ]; then
	usage
	exit 2
fi

target=$1
backup_path=$2
shift 2
print_commands=0

while [ $# -gt 0 ]; do
	case "$1" in
	--print-commands)
		print_commands=1
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown option: $1" >&2
		usage
		exit 2
		;;
	esac
	shift
done

case "$backup_path" in
*"'"* | *" "* | *"	"* | *'
'*)
	echo "backup path contains unsupported whitespace or quote characters" >&2
	exit 2
	;;
esac

remote_quote() {
	printf "'"
	printf '%s' "$1" | sed "s/'/'\\\\''/g"
	printf "'"
}

shell_quote() {
	printf "'"
	printf '%s' "$1" | sed "s/'/'\\\\''/g"
	printf "'"
}

run_remote() {
	if [ "$print_commands" = "1" ]; then
		printf 'ssh %s %s\n' "$(shell_quote "$target")" "$(shell_quote "$1")"
	else
		ssh "$target" "$1"
	fi
}

backup_q=$(remote_quote "$backup_path")

run_remote "[ -d $backup_q ] || { echo 'backup path is missing' >&2; exit 1; }"
run_remote "if command -v docker >/dev/null 2>&1; then docker rm -f slipway_live_production_caddy >/dev/null 2>&1 || true; fi"
run_remote "if [ -d $backup_q/etc/caddy ]; then if [ -e /etc/caddy ]; then current_backup=\"/root/slipway-backups/restore-current-\$(date -u +%Y%m%dT%H%M%SZ)\"; mkdir -p \"\$current_backup/etc\"; cp -a /etc/caddy \"\$current_backup/etc/\"; printf 'current_etc_caddy_backup=%s/etc/caddy\n' \"\$current_backup\"; fi; rm -rf /etc/caddy; mkdir -p /etc; cp -a $backup_q/etc/caddy /etc/caddy; else echo 'backup does not contain /etc/caddy; leaving current /etc/caddy untouched' >&2; fi"
run_remote "if command -v systemctl >/dev/null 2>&1; then systemctl daemon-reload || true; if systemctl --no-pager cat caddy >/dev/null 2>&1; then systemctl enable caddy; systemctl restart caddy; else echo 'system Caddy service was not found; restore of files is complete' >&2; fi; else echo 'systemctl is not available; restore of files is complete' >&2; fi"

printf 'restored_from=%s\n' "$backup_path"
