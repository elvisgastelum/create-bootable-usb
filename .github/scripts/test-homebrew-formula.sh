#!/usr/bin/env bash
# Installs Formula/create-bootable-usb.rb from this checkout the way users do
# (tap, then install by full name), audits it and runs its test. It removes
# the formula and the tap again when it finishes.
set -euo pipefail

tap=elvisgastelum/create-bootable-usb
formula=$tap/create-bootable-usb
export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_CLEANUP=1 HOMEBREW_NO_ENV_HINTS=1

if brew tap | grep -x "$tap" >/dev/null; then
	echo "$tap is already tapped here; untap it first." >&2
	exit 1
fi

# A tap is a git repository: commit only the formula under test into one.
repo=$(mktemp -d)
cleanup() {
	brew uninstall --force "$formula" >/dev/null 2>&1 || true
	brew untap "$tap" >/dev/null 2>&1 || true
	rm -rf "$repo"
}
trap cleanup EXIT

mkdir "$repo/Formula"
cp Formula/create-bootable-usb.rb "$repo/Formula/"
git -C "$repo" init --quiet
git -C "$repo" add Formula
git -C "$repo" -c user.name=ci -c user.email=ci@localhost commit --quiet -m "test: formula under test"

brew tap "$tap" "$repo"
brew install "$formula"
brew audit --strict --online "$formula"
brew test "$formula"
create-bootable-usb --version
