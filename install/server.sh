#!/bin/sh
# Installs or updates the LinCos agent on this Linux server, as a systemd service.
# Download it, read it, then run it:
#
#   curl -fsSLO https://raw.githubusercontent.com/ravaee/Mo.LinCos/main/install/server.sh
#   less server.sh
#   sudo sh server.sh
#
# It installs only files whose SHA-256 matches the release's SHA256SUMS.
# Run it again to update. To remove it:
#   systemctl disable --now lincos && rm /usr/local/bin/lincos /etc/systemd/system/lincos.service
set -eu

RELEASE=https://github.com/ravaee/Mo.LinCos/releases/latest/download

fail() { echo "lincos: $*" >&2; exit 1; }
download() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else fail "needs curl or wget"; fi
}
# Checks a download against the release's SHA256SUMS, so a broken or swapped
# file is never installed
verify() {
  want=$(awk -v f="$1" '$2 == f { print $1 }' "$tmp/SHA256SUMS")
  [ -n "$want" ] || fail "$1 is not listed in SHA256SUMS"
  if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/$1" | cut -d' ' -f1)
  else got=$(shasum -a 256 "$tmp/$1" | cut -d' ' -f1); fi
  [ "$got" = "$want" ] || fail "$1 does not match its checksum; nothing was installed"
}

[ "$(uname -s)" = Linux ] || fail "this installs the server agent on Linux; for your own computer use install/local.sh"
[ "$(id -u)" = 0 ] || fail "run it as root: sudo sh server.sh"
command -v systemctl >/dev/null 2>&1 || fail "needs systemd"
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) fail "unsupported processor $(uname -m)" ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
bin=lincos-linux-$arch
download "$RELEASE/SHA256SUMS" "$tmp/SHA256SUMS"
download "$RELEASE/$bin" "$tmp/$bin"
download "$RELEASE/lincos.service" "$tmp/lincos.service"
verify "$bin"
verify lincos.service

# Moved into place rather than written over, so a running agent can be replaced
install -m 755 "$tmp/$bin" /usr/local/bin/lincos.new
mv -f /usr/local/bin/lincos.new /usr/local/bin/lincos
install -m 644 "$tmp/lincos.service" /etc/systemd/system/lincos.service
systemctl daemon-reload
systemctl enable lincos >/dev/null 2>&1
systemctl restart lincos

sleep 1
systemctl is-active --quiet lincos || fail "the service did not start; see: journalctl -u lincos"
echo "LinCos $(/usr/local/bin/lincos version | cut -d' ' -f2) is running on $(hostname)."
echo "From your computer: lincos open <this server>"
