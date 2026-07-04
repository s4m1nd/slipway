#!/bin/sh
set -eu

usage() {
	cat >&2 <<'EOF'
usage: scripts/live/prepare-server.sh <user@host> [--stop-system-caddy] [--purge-system-caddy] [--print-commands]

Default behavior is inspect + backup only.
--stop-system-caddy stops and disables the system Caddy service after backup.
--purge-system-caddy purges the apt Caddy package after backup and requires --stop-system-caddy.
--print-commands prints the SSH commands without executing them.
EOF
}

if [ $# -lt 1 ]; then
	usage
	exit 2
fi

target=$1
shift
stop_system_caddy=0
purge_system_caddy=0
print_commands=0

while [ $# -gt 0 ]; do
	case "$1" in
	--stop-system-caddy)
		stop_system_caddy=1
		;;
	--purge-system-caddy)
		purge_system_caddy=1
		;;
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

if [ "$purge_system_caddy" = "1" ] && [ "$stop_system_caddy" != "1" ]; then
	echo "--purge-system-caddy requires --stop-system-caddy" >&2
	exit 2
fi

if [ "$stop_system_caddy" = "0" ]; then
	echo "Inspecting and backing up only. Pass --stop-system-caddy to stop host Caddy." >&2
fi
if [ "$purge_system_caddy" = "1" ]; then
	echo "WARNING: --purge-system-caddy is destructive and removes the apt Caddy package after backup." >&2
fi

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

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_dir="/root/slipway-backups/$timestamp"
backup_q=$(remote_quote "$backup_dir")
diag_q=$(remote_quote "$backup_dir/diagnostics")

run_remote "mkdir -p $diag_q"
run_remote "uname -a > $diag_q/uname.txt 2>&1 || true"
run_remote "docker --version > $diag_q/docker-version.txt 2>&1 || true"
run_remote "caddy version > $diag_q/caddy-version.txt 2>&1 || true"
run_remote "systemctl --no-pager status caddy > $diag_q/systemctl-status-caddy.txt 2>&1 || true"
run_remote "systemctl --no-pager cat caddy > $diag_q/systemctl-cat-caddy.txt 2>&1 || true"
run_remote "ss -ltnp > $diag_q/ss-ltnp.txt 2>&1 || true"
run_remote "dpkg -l caddy > $diag_q/dpkg-caddy.txt 2>&1 || true"

run_remote "if [ -e /etc/caddy ]; then mkdir -p $backup_q/etc; cp -a /etc/caddy $backup_q/etc/; fi"
run_remote "if [ -e /var/lib/caddy ]; then mkdir -p $backup_q/var/lib; cp -a /var/lib/caddy $backup_q/var/lib/; fi"
run_remote "if [ -e /var/log/caddy ]; then mkdir -p $backup_q/var/log; cp -a /var/log/caddy $backup_q/var/log/; fi"

if [ "$stop_system_caddy" = "1" ]; then
	run_remote "if command -v systemctl >/dev/null 2>&1; then systemctl stop caddy >/dev/null 2>&1 || true; systemctl disable caddy >/dev/null 2>&1 || true; else echo 'systemctl is not available; cannot stop system Caddy' >&2; exit 1; fi"
fi

if [ "$purge_system_caddy" = "1" ]; then
	run_remote "if command -v apt-get >/dev/null 2>&1; then if dpkg -s caddy >/dev/null 2>&1; then DEBIAN_FRONTEND=noninteractive apt-get purge -y caddy; fi; else echo 'apt-get is not available; cannot purge system Caddy' >&2; exit 1; fi"
fi

printf 'backup_path=%s\n' "$backup_dir"
