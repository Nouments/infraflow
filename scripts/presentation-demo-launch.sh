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

printf '\n== Generate and verify Cisco OSPF configuration ==\n'
run_provider generate-device-config \
	-f examples/network-deployment-ospf.yaml \
	-out "$demo_root/ospf"
if ! grep -Fq 'router ospf 10' "$demo_root/ospf/demo/R1/config.cfg"; then
	printf 'OSPF configuration was not generated.\n' >&2
	exit 1
fi
printf 'PASS: Cisco OSPF configuration rendered; device application is not claimed.\n'

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
	printf 'root=%s\n' "$demo_root" >>"$GITHUB_OUTPUT"
fi
printf '\nPresentation demo generation complete. Output retained at: %s\n' "$demo_root"
