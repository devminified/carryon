#!/bin/sh
# CO-308: fills in the 3 .yaml.tmpl manifests in this directory for a
# real release, from a release's own checksums.txt — run after
# `goreleaser release` has produced dist/checksums.txt and the real
# release assets are uploaded (the InstallerUrl below assumes
# carryon_windows_amd64.msi is already live at that URL; this script
# only generates the manifest text, it never submits anything).
#
# Usage: packaging/winget/generate.sh <version> <checksums.txt path> <out-dir>
#
# Submitting the generated 3 files as a PR to microsoft/winget-pkgs is
# the external, deferred step this ticket's own scope decision left
# for later — see docs/co-308-fleet-rollout.md.
set -eu

VERSION="${1:?usage: generate.sh <version> <checksums.txt path> <out-dir>}"
CHECKSUMS="${2:?}"
OUT_DIR="${3:?}"
BASE_URL="https://github.com/devminified/carryon/releases/download/v${VERSION}"

SHA256_WINDOWS_AMD64="$(awk '/carryon_windows_amd64\.msi$/ {print $1}' "$CHECKSUMS")"
if [ -z "$SHA256_WINDOWS_AMD64" ]; then
  echo "generate.sh: no carryon_windows_amd64.msi entry in $CHECKSUMS" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
for f in Devminified.Carryon.yaml Devminified.Carryon.installer.yaml Devminified.Carryon.locale.en-US.yaml; do
  sed \
    -e "s/{{VERSION}}/${VERSION}/g" \
    -e "s#{{BASE_URL}}#${BASE_URL}#g" \
    -e "s/{{SHA256_WINDOWS_AMD64}}/${SHA256_WINDOWS_AMD64}/g" \
    "$(dirname "$0")/${f}.tmpl" > "${OUT_DIR}/${f}"
  echo "wrote ${OUT_DIR}/${f}"
done
