#!/usr/bin/env bash
set -euo pipefail

go_bin_path="$(go env GOPATH)/bin"
export PATH="$go_bin_path:$PATH"

required_tools=(git go make)
optional_tools=(dupl gitnexus go-arch-lint golangci-lint)
missing_required=0

for tool in "${required_tools[@]}"; do
	if command -v "$tool" >/dev/null 2>&1; then
		printf '[required] %-16s %s\n' "$tool" "ok"
	else
		printf '[required] %-16s %s\n' "$tool" "missing"
		missing_required=1
	fi
done

for tool in "${optional_tools[@]}"; do
	if command -v "$tool" >/dev/null 2>&1; then
		printf '[quality]  %-16s %s\n' "$tool" "ok"
	else
		printf '[quality]  %-16s %s\n' "$tool" "missing (install with 'make tools')"
	fi
done

exit "$missing_required"
