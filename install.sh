#!/bin/sh
# cc360 installer
#
#   curl -fsSL https://raw.githubusercontent.com/achton/cc360/main/install.sh | sh
#
# Environment variables:
#   CC360_VERSION       version to install, e.g. "v0.3.0" (default: latest release)
#   CC360_INSTALL_DIR   install directory (default: /usr/local/bin)
#
# A version may also be passed as the first argument:
#   curl -fsSL .../install.sh | sh -s -- v0.3.0

set -eu

REPO="achton/cc360"
BIN="cc360"
INSTALL_DIR="${CC360_INSTALL_DIR:-/usr/local/bin}"
VERSION="${1:-${CC360_VERSION:-}}"

err() { printf 'error: %s\n' "$*" >&2; exit 1; }
info() { printf '%s\n' "$*" >&2; }

# Pick a downloader.
if command -v curl >/dev/null 2>&1; then
	dl() { curl -fsSL "$1"; }
	dl_to() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	dl() { wget -qO- "$1"; }
	dl_to() { wget -qO "$2" "$1"; }
else
	err "neither curl nor wget found"
fi

# Detect OS.
os=$(uname -s)
case "$os" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) err "unsupported OS: $os (cc360 ships linux and darwin builds)" ;;
esac

# Detect architecture and map to GoReleaser's naming.
arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) err "unsupported architecture: $arch (cc360 ships amd64 and arm64 builds)" ;;
esac

# Resolve the latest version if none was requested.
if [ -z "$VERSION" ]; then
	info "Resolving latest release..."
	VERSION=$(dl "https://api.github.com/repos/${REPO}/releases/latest" |
		grep '"tag_name":' | head -n1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
	[ -n "$VERSION" ] || err "could not determine latest version"
fi

# Release assets use the version without a leading "v" (e.g. cc360_0.3.0_linux_amd64.tar.gz).
ver_no_v=${VERSION#v}
archive="${BIN}_${ver_no_v}_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${VERSION}"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

info "Downloading ${archive} (${VERSION})..."
dl_to "${base}/${archive}" "${tmp}/${archive}" || err "download failed: ${base}/${archive}"

# Verify the checksum when a sha256 tool is available.
if dl_to "${base}/checksums.txt" "${tmp}/checksums.txt" 2>/dev/null; then
	if command -v sha256sum >/dev/null 2>&1; then
		sha=$(sha256sum "${tmp}/${archive}" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		sha=$(shasum -a 256 "${tmp}/${archive}" | awk '{print $1}')
	else
		sha=""
		info "warning: no sha256 tool found, skipping checksum verification"
	fi
	if [ -n "$sha" ]; then
		want=$(grep " ${archive}\$" "${tmp}/checksums.txt" | awk '{print $1}')
		[ -n "$want" ] || err "checksum for ${archive} not found in checksums.txt"
		[ "$sha" = "$want" ] || err "checksum mismatch for ${archive}"
		info "Checksum verified."
	fi
else
	info "warning: checksums.txt unavailable, skipping verification"
fi

tar -xzf "${tmp}/${archive}" -C "$tmp" "$BIN" || err "failed to extract $BIN"

# Install, elevating with sudo only if the target dir is not writable.
target="${INSTALL_DIR}/${BIN}"
if [ -w "$INSTALL_DIR" ] || { [ ! -e "$INSTALL_DIR" ] && mkdir -p "$INSTALL_DIR" 2>/dev/null; }; then
	install -m 0755 "${tmp}/${BIN}" "$target"
elif command -v sudo >/dev/null 2>&1; then
	info "Installing to ${INSTALL_DIR} (requires sudo)..."
	sudo install -d -m 0755 "$INSTALL_DIR"
	sudo install -m 0755 "${tmp}/${BIN}" "$target"
else
	err "cannot write to ${INSTALL_DIR} and sudo is unavailable; set CC360_INSTALL_DIR to a writable path"
fi

info "Installed ${BIN} ${VERSION} to ${target}"

# Warn if the install dir is not on PATH.
case ":${PATH}:" in
	*":${INSTALL_DIR}:"*) ;;
	*) info "note: ${INSTALL_DIR} is not on your PATH" ;;
esac
