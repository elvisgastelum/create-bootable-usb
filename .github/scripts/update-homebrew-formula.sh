#!/usr/bin/env bash
# Points Formula/create-bootable-usb.rb at a release and commits the change on
# the current branch (the release workflow runs it on main, then pushes):
#
#   .github/scripts/update-homebrew-formula.sh v0.0.2 path/to/checksums.txt
#
# Pre-releases and versions older than the one in the formula are skipped.
set -euo pipefail

usage="usage: update-homebrew-formula.sh <tag> <checksums.txt>"
tag=${1:?$usage}
checksums=${2:?$usage}
formula=Formula/create-bootable-usb.rb
version=${tag#v}

case $version in
*-*)
	echo "Skipping pre-release $tag: the formula follows stable releases."
	exit 0
	;;
esac

current=$(sed -n 's|.*/releases/download/v\([^/]*\)/.*|\1|p' "$formula" | head -n 1)
newest=$(printf '%s\n%s\n' "$current" "$version" | sort -V | tail -n 1)
if [ "$newest" != "$version" ]; then
	echo "Skipping $tag: the formula already points at the newer v$current."
	exit 0
fi

go run ./tools/brewformula -version "$version" -checksums "$checksums" -o "$formula"
if git diff --quiet -- "$formula"; then
	echo "The formula already points at $tag."
	exit 0
fi

git add -- "$formula"
git -c user.name="github-actions[bot]" \
	-c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
	commit --quiet -m "chore(brew): update formula to v$version"
echo "Committed the formula for $tag."
