#!/usr/bin/env bash
# Local QA gate: runs every check that needs no lab hardware and prints a
# machine-readable summary line per gate ("GATE <name> PASS|FAIL <seconds>").
# Usage: scripts/qa/gate.sh [output-dir]
set -u
cd "$(dirname "$0")/../.."
out=${1:-$(mktemp -d)}
mkdir -p "$out"
export GOTOOLCHAIN=${GOTOOLCHAIN:-local}
status=0
run() {
  local name=$1; shift
  local start=$SECONDS
  if "$@" >"$out/$name.txt" 2>&1; then r=PASS; else r=FAIL; status=1; fi
  echo "GATE $name $r $((SECONDS-start))s" | tee -a "$out/summary.txt"
}
gofmt_check() { test -z "$(gofmt -l $(git ls-files '*.go'; git ls-files --others --exclude-standard '*.go'))"; }
build() { go build -o /dev/null ./provider/cmd && go build -o /dev/null ./agent/cmd; }
coverage() {
  go test -count=1 -coverprofile="$out/proc.cover" ./agent/internal/adapters/processor >/dev/null &&
  go test -count=1 -coverprofile="$out/cli.cover" ./agent/internal/delivery/cli >/dev/null &&
  p=$(go tool cover -func="$out/proc.cover" | awk '/^total/{sub("%","",$3);print $3}') &&
  c=$(go tool cover -func="$out/cli.cover" | awk '/^total/{sub("%","",$3);print $3}') &&
  echo "processor=$p cli=$c" && awk -v p="$p" -v c="$c" 'BEGIN{exit !(p>=90 && c>=65)}'
}
web() {
  node --check provider/internal/delivery/web/assets/app.js &&
  node --check provider/internal/delivery/web/assets/log-stream.js &&
  node --test provider/internal/delivery/web/assets/log-stream.test.cjs
}
secrets() {
  # Committed files must not carry private keys or obvious credential assignments.
  ! git grep -nIE -- '-----BEGIN [A-Z ]*PRIVATE KEY-----|(password|secret|token)[[:space:]]*[:=][[:space:]]*"[^"$<{]{8,}"' \
      -- ':!*_test.go' ':!infraflow-network-reference' ':!docs' ':!*.md'
}
perms() { ! git ls-files -s | awk '$1=="100755"{print $4}' | grep -vE '\.(sh|py)$|^scripts/|^bin/'; }
run gofmt gofmt_check
run vet go vet ./...
run test go test -count=1 ./...
run race go test -race -count=1 ./...
run build build
run vuln govulncheck ./...
run coverage coverage
run web web
run shell bash scripts/cisco-ios-preflight_test.sh
run secrets secrets
run perms perms
run diffcheck git diff --check
echo "OVERALL $([ $status = 0 ] && echo PASS || echo FAIL)" | tee -a "$out/summary.txt"
exit $status
