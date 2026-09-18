#!/bin/sh
# pando installer.
#
#   curl -fsSL https://raw.githubusercontent.com/xseman/pando/master/install.sh | sh
#
# PANDO_VERSION pins a release (default: the latest), PANDO_INSTALL_DIR the
# directory the binary lands in (default: ~/.local/bin).
set -eu

REPO="xseman/pando"
BIN="pando"
INSTALL_DIR="${PANDO_INSTALL_DIR:-$HOME/.local/bin}"

main() {
	echo ""
	echo "   ⌁ pando installer"
	echo "     github.com/$REPO"
	echo ""

	case "$(uname -s)" in
		Linux) os="linux" ;;
		Darwin) os="darwin" ;;
		*) err "unsupported OS: $(uname -s) — pando runs on Linux and macOS" ;;
	esac
	case "$(uname -m)" in
		x86_64 | amd64) arch="amd64" ;;
		aarch64 | arm64) arch="arm64" ;;
		*) err "unsupported architecture: $(uname -m)" ;;
	esac
	asset="${BIN}-${os}-${arch}"
	log "detected ${os}/${arch}"

	need curl
	need awk
	sha_tool

	tag="${PANDO_VERSION:-}"
	case "$tag" in
		"")
			# The /releases/latest redirect names the newest tag, so the
			# install needs no GitHub API token and hits no rate limit.
			latest="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")" ||
				err "can't reach github.com — try again later"
			tag="${latest##*/}"
			;;
		v*) ;;
		*) tag="v${tag}" ;;
	esac
	[ -n "$tag" ] || err "could not work out the latest release"
	base="https://github.com/${REPO}/releases/download/${tag}"

	tmp="$(mktemp -d)"
	trap 'rm -rf "$tmp"' EXIT

	log "fetching checksums for ${tag}..."
	curl -fsSL --retry 3 --connect-timeout 10 --max-time 30 "${base}/CHECKSUMS.txt" -o "${tmp}/CHECKSUMS.txt" ||
		err "release ${tag} has no CHECKSUMS.txt"
	want="$(awk -v a="$asset" '$2 == a || $2 == "*" a { print $1; exit }' "${tmp}/CHECKSUMS.txt")"
	[ "${#want}" -eq 64 ] || err "release ${tag} does not ship ${asset}"

	log "downloading ${asset} ${tag}..."
	curl -fSL --progress-bar --retry 3 --connect-timeout 10 --max-time 300 "${base}/${asset}" -o "${tmp}/${BIN}" ||
		err "download failed from ${base}/${asset}"

	got="$(sha256 "${tmp}/${BIN}")"
	[ "$got" = "$want" ] || err "checksum mismatch: got ${got}, want ${want}"

	mkdir -p "$INSTALL_DIR"
	mv "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}"
	chmod 0755 "${INSTALL_DIR}/${BIN}"
	log "installed ${BIN} ${tag} to ${INSTALL_DIR}/${BIN}"

	case ":${PATH}:" in
		*":${INSTALL_DIR}:"*) ;;
		*)
			echo ""
			warn "${INSTALL_DIR} is not in your PATH; add this to your shell config:"
			echo ""
			echo "    export PATH=\"${INSTALL_DIR}:\$PATH\""
			;;
	esac

	echo ""
	log "run '${BIN}' to open the TUI, '${BIN} help' for the CLI"
	echo ""
}

log() { printf '  \033[32m>\033[0m %s\n' "$1"; }
warn() { printf '  \033[33m!\033[0m %s\n' "$1"; }
err() {
	printf '  \033[31mx\033[0m %s\n' "$1" >&2
	exit 1
}

need() {
	command -v "$1" > /dev/null 2>&1 || err "requires '$1' — install it first"
}

# sha_tool picks whatever this machine has; the download is never installed
# unverified.
sha_tool() {
	for t in sha256sum shasum openssl; do
		if command -v "$t" > /dev/null 2>&1; then
			SHA_TOOL="$t"
			return
		fi
	done
	err "requires sha256sum, shasum or openssl to verify the download"
}

sha256() {
	case "$SHA_TOOL" in
		sha256sum) sha256sum < "$1" | awk '{ print $1 }' ;;
		shasum) shasum -a 256 < "$1" | awk '{ print $1 }' ;;
		openssl) openssl dgst -sha256 < "$1" | awk '{ print $NF }' ;;
	esac
}

main "$@"
