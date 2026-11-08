#!/usr/bin/env bash
# Offline regression test for cisco-ios-preflight.sh input validation.
# Every case must exit 64 BEFORE any network access.
set -uo pipefail

script="$(dirname "$0")/cisco-ios-preflight.sh"
fp='SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'
failures=0

expect_64() {
	local name=$1
	shift
	bash "$script" "$@" >/dev/null 2>&1
	local code=$?
	if [[ $code -eq 64 ]]; then
		printf 'PASS %s\n' "$name"
	else
		printf 'FAIL %s (exit %s, expected 64)\n' "$name" "$code"
		failures=$((failures + 1))
	fi
}

expect_64 "no arguments"
expect_64 "too many arguments" h u "$fp" 22 extra
expect_64 "invalid host" 'bad host' u "$fp"
expect_64 "host with shell metachar" 'h;id' u "$fp"
expect_64 "invalid user" h 'u;x' "$fp"
expect_64 "fingerprint without SHA256 prefix" h u abc
expect_64 "fingerprint with MD5 format" h u 'MD5:aa:bb'
expect_64 "port zero" h u "$fp" 0
expect_64 "port too large" h u "$fp" 70000
expect_64 "port not numeric" h u "$fp" abc

if bash -n "$script"; then
	printf 'PASS bash syntax\n'
else
	printf 'FAIL bash syntax\n'
	failures=$((failures + 1))
fi

if [[ $failures -ne 0 ]]; then
	printf '%s failure(s)\n' "$failures"
	exit 1
fi
printf 'All preflight script checks passed.\n'