#!/usr/bin/env bash
set -euo pipefail

usage() {
	printf 'Usage: %s <host> <ssh-user> <expected-SHA256-fingerprint> [port]\n' "$0" >&2
	printf 'Requires an independently verified host-key fingerprint and SSH public-key auth.\n' >&2
}

if [[ $# -lt 3 || $# -gt 4 ]]; then
	usage
	exit 64
fi

host=$1
ssh_user=$2
expected_fingerprint=$3
port=${4:-22}

if [[ ! $host =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]]; then
	printf 'Invalid host name or IPv4 address.\n' >&2
	exit 64
fi
if [[ ! $ssh_user =~ ^[A-Za-z0-9_.-]+$ ]]; then
	printf 'Invalid SSH user name.\n' >&2
	exit 64
fi
if [[ ! $expected_fingerprint =~ ^SHA256:[A-Za-z0-9+/]+={0,2}$ ]]; then
	printf 'Expected fingerprint must use OpenSSH SHA256: format.\n' >&2
	exit 64
fi
if [[ ! $port =~ ^[0-9]+$ ]] || (( port < 1 || port > 65535 )); then
	printf 'Port must be an integer from 1 to 65535.\n' >&2
	exit 64
fi

for tool in ssh ssh-keyscan ssh-keygen awk grep sort; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		printf 'Required tool not found: %s\n' "$tool" >&2
		exit 69
	fi
done

known_hosts=$(mktemp)
chmod 600 "$known_hosts"
trap 'rm -f "$known_hosts"' EXIT

if ! ssh-keyscan -T 5 -p "$port" "$host" >"$known_hosts" 2>/dev/null; then
	printf 'SSH host-key scan failed for %s:%s.\n' "$host" "$port" >&2
	exit 1
fi
if [[ ! -s $known_hosts ]]; then
	printf 'No SSH host key was returned by %s:%s.\n' "$host" "$port" >&2
	exit 1
fi

fingerprints=$(ssh-keygen -lf "$known_hosts" -E sha256 2>/dev/null | awk '{print $2}' | sort -u)
if ! grep -Fxq "$expected_fingerprint" <<<"$fingerprints"; then
	printf 'SSH host-key fingerprint mismatch for %s:%s.\n' "$host" "$port" >&2
	exit 1
fi

ssh_options=(
	-F /dev/null
	-o BatchMode=yes
	-o ConnectTimeout=5
	-o GlobalKnownHostsFile=/dev/null
	-o KbdInteractiveAuthentication=no
	-o LogLevel=ERROR
	-o PasswordAuthentication=no
	-o PreferredAuthentications=publickey
	-o StrictHostKeyChecking=yes
	-o UserKnownHostsFile="$known_hosts"
	-p "$port"
)
if ! version_output=$(ssh "${ssh_options[@]}" "$ssh_user@$host" 'show version' 2>/dev/null); then
	printf 'Read-only SSH command failed for %s:%s; verify SSH-agent/public-key access.\n' "$host" "$port" >&2
	exit 1
fi

version_lines=$(printf '%s\n' "$version_output" | grep -E 'Cisco IOS( XE)? Software|Cisco Internetwork Operating System Software' || true)
if [[ -z $version_lines ]]; then
	printf 'SSH succeeded, but no Cisco IOS/IOS XE version line was recognized.\n' >&2
	exit 1
fi

printf 'SSH reachability: PASS (%s:%s)\n' "$host" "$port"
printf 'SSH host-key fingerprint: PASS (%s)\n' "$expected_fingerprint"
printf 'Cisco software version observed:\n%s\n' "$version_lines"