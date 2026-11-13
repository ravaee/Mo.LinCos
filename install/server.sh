#!/bin/sh
# Installs or updates the LinCos agent on this Linux server, as a systemd service:
#
#   curl -fsSL https://raw.githubusercontent.com/ravaee/Mo.LinCos/main/install/server.sh | sudo sh
#
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

[ "$(uname -s)" = Linux ] || fail "this installs the server agent on Linux; for your own computer use install/local.sh"
[ "$(id -u)" = 0 ] || fail "run it as root: ... | sudo sh"
command -v systemctl >/dev/null 2>&1 || fail "needs systemd"
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) fail "unsupported processor $(uname -m)" ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
download "$RELEASE/lincos-linux-$arch" "$tmp/lincos"
download "$RELEASE/lincos.service" "$tmp/lincos.service"

# Moved into place rather than written over, so a running agent can be replaced
install -m 755 "$tmp/lincos" /usr/local/bin/lincos.new
mv -f /usr/local/bin/lincos.new /usr/local/bin/lincos
install -m 644 "$tmp/lincos.service" /etc/systemd/system/lincos.service
systemctl daemon-reload
systemctl enable lincos >/dev/null 2>&1
systemctl restart lincos

sleep 1
systemctl is-active --quiet lincos || fail "the service did not start; see: journalctl -u lincos"
echo "LinCos $(/usr/local/bin/lincos version | cut -d' ' -f2) is running on $(hostname)."
echo "From your computer: lincos open <this server>"
