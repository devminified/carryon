#!/bin/sh
# CO-308: curl -fsSL https://raw.githubusercontent.com/devminified/carryon/main/install.sh | sh
#
# Downloads the right carryon archive for this OS/arch from the latest
# GitHub release, verifies it against the release's own checksums.txt
# (never trusts a download that isn't independently verified), extracts
# it into $CARRYON_INSTALL_DIR (default: $HOME/.local/bin), and runs
# `carryon install` to finish setup.
#
# CARRYON_INSTALL_BASE_URL overrides where the archive/checksums.txt
# are fetched from — real installs never need this; it exists so this
# script itself can be tested end to end against a local fixture
# server instead of a real GitHub release.
set -eu

BASE_URL="${CARRYON_INSTALL_BASE_URL:-https://github.com/devminified/carryon/releases/latest/download}"
INSTALL_DIR="${CARRYON_INSTALL_DIR:-$HOME/.local/bin}"

info() { printf 'carryon install: %s\n' "$1"; }
die() {
  printf 'carryon install: error: %s\n' "$1" >&2
  exit 1
}

detect_os() {
  case "$(uname -s)" in
    Darwin) echo darwin ;;
    Linux) echo linux ;;
    *) die "unsupported OS: $(uname -s) (carryon ships darwin/linux/windows only)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo amd64 ;;
    arm64 | aarch64) echo arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
  esac
}

OS="$(detect_os)"
ARCH="$(detect_arch)"
ARCHIVE="carryon_${OS}_${ARCH}.tar.gz"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

info "downloading ${ARCHIVE} from ${BASE_URL}"
curl -fsSL "${BASE_URL}/${ARCHIVE}" -o "${WORKDIR}/${ARCHIVE}"
curl -fsSL "${BASE_URL}/checksums.txt" -o "${WORKDIR}/checksums.txt"

EXPECTED_LINE="$(grep "  ${ARCHIVE}\$" "${WORKDIR}/checksums.txt" || true)"
if [ -z "$EXPECTED_LINE" ]; then
  die "checksums.txt has no entry for ${ARCHIVE} — refusing to install an unverifiable download"
fi

info "verifying checksum"
cd "$WORKDIR"
if command -v sha256sum >/dev/null 2>&1; then
  echo "$EXPECTED_LINE" | sha256sum -c - || die "checksum verification failed for ${ARCHIVE} — the download may be corrupted or tampered with"
elif command -v shasum >/dev/null 2>&1; then
  echo "$EXPECTED_LINE" | shasum -a 256 -c - || die "checksum verification failed for ${ARCHIVE} — the download may be corrupted or tampered with"
else
  die "neither sha256sum nor shasum is available — cannot verify the download, refusing to install unverified"
fi

info "extracting"
tar -xzf "${ARCHIVE}"

mkdir -p "$INSTALL_DIR"
mv carryon "$INSTALL_DIR/carryon"
chmod +x "$INSTALL_DIR/carryon"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) info "note: $INSTALL_DIR is not on your PATH — add it to your shell profile" ;;
esac

info "running 'carryon install' to finish setup"
"$INSTALL_DIR/carryon" install

info "done — $("$INSTALL_DIR/carryon" version)"
