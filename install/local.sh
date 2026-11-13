#!/bin/sh
# Installs or updates the lincos command on your Mac or Linux computer:
#
#   curl -fsSL https://raw.githubusercontent.com/ravaee/Mo.LinCos/main/install/local.sh | sh
#
# Then: lincos open <any server you can ssh to that runs the agent>
set -eu

RELEASE=https://github.com/ravaee/Mo.LinCos/releases/latest/download

fail() { echo "lincos: $*" >&2; exit 1; }
download() {
  if command -v curl >/dev/null 2>&1; then curl -fsSL -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else fail "needs curl or wget"; fi
}

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) fail "unsupported system $(uname -s); on Windows use install/local.ps1" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) fail "unsupported processor $(uname -m)" ;;
esac

# The first folder on the PATH it can write to; otherwise /usr/local/bin with sudo
dest="" sudo=""
for d in /usr/local/bin /opt/homebrew/bin "$HOME/.local/bin" "$HOME/bin"; do
  case ":$PATH:" in *":$d:"*) ;; *) continue ;; esac
  if [ -d "$d" ] && [ -w "$d" ]; then dest=$d; break; fi
done
if [ -z "$dest" ]; then
  dest=/usr/local/bin sudo=sudo
  echo "Installing to $dest needs your password."
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
download "$RELEASE/lincos-$os-$arch" "$tmp/lincos"
$sudo mkdir -p "$dest"
$sudo install -m 755 "$tmp/lincos" "$dest/lincos.new"
$sudo mv -f "$dest/lincos.new" "$dest/lincos"

echo "Installed $("$dest/lincos" version) to $dest/lincos"
command -v ssh >/dev/null 2>&1 || echo "Note: lincos needs ssh, which was not found."
echo "Next: lincos open <ssh host>"
