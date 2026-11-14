#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
cd "$repo_root"

for tool in go python3; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		printf 'Required command not found: %s\n' "$tool" >&2
		exit 69
	fi
done

demo_root="$(mktemp -d "${TMPDIR:-/tmp}/infraflow-presentation-demo.XXXXXXXX")"
on_error() {
	status=$?
	trap - ERR
	printf 'Presentation demo failed (exit %s); partial output retained at: %s\n' "$status" "$demo_root" >&2
	exit "$status"
}
trap on_error ERR

run_provider() {
	printf '\n$ go run ./provider/cmd'
	printf ' %q' "$@"
	printf '\n'
	go run ./provider/cmd "$@"
}

expect_validation() {
	input=$1
	expected=$2
	output="$(go run ./provider/cmd validate -f "$input")"
	printf '%s\n' "$output"
	if [[ "$output" != "$expected" ]]; then
		printf 'Unexpected validation output for %s: %s\n' "$input" "$output" >&2
		return 1
	fi
}

printf 'Fresh demo output root: %s\n' "$demo_root"
printf '\n== Validate and plan the Cisco/MikroTik generation input ==\n'
expect_validation examples/provisioning-generation.yaml \
	'Valid: 2 site(s), 2 device(s); no infrastructure changes made.'
run_provider plan -f examples/provisioning-generation.yaml

printf '\n== Generate native Cisco and MikroTik artifacts ==\n'
run_provider generate-all -f examples/provisioning-generation.yaml -out "$demo_root/provisioning"

printf '\n== Generate opt-in bootstrap artifacts ==\n'
expect_validation examples/infra.yaml \
	'Valid: 1 site(s), 2 device(s); no infrastructure changes made.'
run_provider generate-all -f examples/infra.yaml -out "$demo_root/bootstrap-enabled"

printf '\n== Generate multisite output with bootstrap explicitly disabled ==\n'
expect_validation examples/multisite-preparation.yaml \
	'Valid: 4 site(s), 5 device(s); no infrastructure changes made.'
run_provider generate-all -f examples/multisite-preparation.yaml -out "$demo_root/multisite-disabled"

printf '\n== Confirm unsupported OSPF fails closed ==\n'
rejection_log="$demo_root/unsupported-ospf.log"
if go run ./provider/cmd generate-device-config \
	-f examples/network-deployment-unsupported-ospf.yaml \
	-out "$demo_root/unsupported-ospf" >"$rejection_log" 2>&1; then
	printf 'Unsupported OSPF unexpectedly succeeded.\n' >&2
	exit 1
fi
expected_error='infraflow-provider: spec.devices[0].routing.ospf: unsupported; no matching OSPF template is registered'
if ! grep -Fxq "$expected_error" "$rejection_log"; then
	printf 'Unsupported OSPF failed with an unexpected error:\n' >&2
	cat "$rejection_log" >&2
	exit 1
fi
if [[ -e "$demo_root/unsupported-ospf" ]]; then
	printf 'Unsupported OSPF published output unexpectedly: %s\n' "$demo_root/unsupported-ospf" >&2
	exit 1
fi
printf '%s\n' "$expected_error"
printf 'PASS: rejected with no output directory.\n'

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
	printf 'root=%s\n' "$demo_root" >>"$GITHUB_OUTPUT"
fi
printf '\nPresentation demo generation complete. Output retained at: %s\n' "$demo_root"
