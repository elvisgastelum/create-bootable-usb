#!/usr/bin/env bash
# Tests update-homebrew-formula.sh in a scratch git repository holding a copy
# of the generator and a formula for v1.0.0. CI runs it on Linux and macOS.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
script=${UPDATE_SCRIPT:-$root/.github/scripts/update-homebrew-formula.sh}
sums=$root/tools/brewformula/testdata/checksums.txt
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

mkdir "$work/repo" "$work/repo/Formula"
cp -R "$root/go.mod" "$root/go.sum" "$root/tools" "$work/repo/"
grep -v linux_arm64 "$sums" >"$work/missing-archive.txt"
cd "$work/repo"
go run ./tools/brewformula -version 1.0.0 -checksums "$sums" -o Formula/create-bootable-usb.rb
git init --quiet
git add -A
git -c user.name=test -c user.email=test@localhost commit --quiet -m "test: v1.0.0 formula"

failures=0
# expect <case> <exit code> <HEAD subject> <formula version> -- <script arguments>
expect() {
	local name=$1 want_code=$2 want_head=$3 want_version=$4 code=0 out head version dirty
	shift 5
	out=$("$script" "$@" 2>&1) || code=$?
	head=$(git log -1 --format=%s)
	version=$(sed -n 's|.*/releases/download/v\([^/]*\)/.*|\1|p' Formula/create-bootable-usb.rb | sort -u | tr '\n' ' ')
	dirty=$(git status --porcelain)
	if [ "$code" = "$want_code" ] && [ "$head" = "$want_head" ] && [ "$version" = "$want_version " ] && [ -z "$dirty" ]; then
		echo "ok   $name"
	else
		echo "FAIL $name: exit $code (want $want_code), HEAD \"$head\" (want \"$want_head\"), formula v$version(want v$want_version), uncommitted: ${dirty:-none}"
		echo "     ${out//$'\n'/$'\n'     }"
		failures=$((failures + 1))
	fi
}

expect "a new release is committed" 0 "chore(brew): update formula to v1.0.1" 1.0.1 -- v1.0.1 "$sums"
expect "the same release again changes nothing" 0 "chore(brew): update formula to v1.0.1" 1.0.1 -- v1.0.1 "$sums"
expect "an older release is skipped" 0 "chore(brew): update formula to v1.0.1" 1.0.1 -- v1.0.0 "$sums"
expect "a pre-release is skipped" 0 "chore(brew): update formula to v1.0.1" 1.0.1 -- v1.1.0-rc.1 "$sums"
expect "versions compare as numbers (1.0.10 > 1.0.2)" 0 "chore(brew): update formula to v1.0.10" 1.0.10 -- v1.0.10 "$sums"
expect "a checksums file without an archive fails and changes nothing" 1 "chore(brew): update formula to v1.0.10" 1.0.10 -- v1.0.11 "$work/missing-archive.txt"

authors=$(git log -2 --format='%an <%ae>' | sort -u)
if [ "$authors" = "github-actions[bot] <41898282+github-actions[bot]@users.noreply.github.com>" ]; then
	echo "ok   commits are made by github-actions[bot]"
else
	echo "FAIL commits are made by: $authors"
	failures=$((failures + 1))
fi

[ "$failures" -eq 0 ] || { echo "$failures failed"; exit 1; }
