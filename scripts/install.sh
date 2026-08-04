#!/usr/bin/env bash
# One-command install for rdev. Works without Go and without admin rights:
# if Go is missing it installs a user-level toolchain under ~/.local.
#
#   curl -fsSL https://raw.githubusercontent.com/EitanWong/remote-dev-skillkit/main/scripts/install.sh | bash
set -euo pipefail

say() { printf '[install] %s\n' "$*"; }

GO_VERSION="go1.25.0"
LOCAL_DIR="${HOME}/.local"

if ! command -v go >/dev/null 2>&1; then
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  case "${OS}-${ARCH}" in
    linux-x86_64)  GO_ARCH="linux-amd64" ;;
    linux-aarch64) GO_ARCH="linux-arm64" ;;
    darwin-x86_64) GO_ARCH="darwin-amd64" ;;
    darwin-arm64)  GO_ARCH="darwin-arm64" ;;
    *) say "unsupported platform: ${OS}-${ARCH}"; exit 1 ;;
  esac
  say "Go is not installed; installing ${GO_VERSION} for ${GO_ARCH} under ${LOCAL_DIR} (no admin needed)..."
  mkdir -p "${LOCAL_DIR}"
  curl -fsSL "https://go.dev/dl/${GO_VERSION}.${GO_ARCH}.tar.gz" \
    | tar -C "${LOCAL_DIR}" -xzf -
  export PATH="${LOCAL_DIR}/go/bin:${PATH}"
  say "Go installed."
else
  say "Go found: $(go version | head -1)"
fi

say "Installing rdev (this compiles a single binary)..."
GOBIN="${LOCAL_DIR}/bin" go install github.com/EitanWong/remote-dev-skillkit/cmd/rdev@latest

BIN="${LOCAL_DIR}/bin/rdev"
if [[ ! -x "${BIN}" ]]; then
  say "installed binary not found at ${BIN}" >&2
  exit 1
fi

say "Done! rdev is at ${BIN}"
say "Next steps:"
say "  - verify:  ${BIN} version"
say "  - if 'rdev' is not on your PATH yet, add ${LOCAL_DIR}/bin to your PATH"
"${BIN}" version
