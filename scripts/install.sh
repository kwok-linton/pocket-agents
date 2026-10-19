#!/bin/sh
# Hosted alongside versioned release archives and SHA256SUMS. No root required.
set -eu
VERSION=${POCKET_VERSION:-0.1.0}
BASE=${POCKET_RELEASE_BASE_URL:-}
ROLE=both
HUB=
CODE=
OPEN=1
while [ "$#" -gt 0 ]; do
 case "$1" in
  --base-url) BASE=$2; shift 2;;
  --version) VERSION=$2; shift 2;;
  --role) ROLE=$2; shift 2;;
  --hub) HUB=$2; shift 2;;
  --code) CODE=$2; shift 2;;
  --no-open) OPEN=0; shift;;
  --help) echo 'install.sh --base-url HTTPS_RELEASE_DIRECTORY [--version VERSION] [--role both|hub|node] [--hub HTTPS_HUB --code PAIR_CODE] [--no-open]'; exit 0;;
  *) echo "Unknown option: $1" >&2; exit 1;;
 esac
done
[ "$(id -u)" -ne 0 ] || { echo 'Run as your normal user, not root or sudo.' >&2; exit 1; }
case "$BASE" in https://*) ;; *) echo 'Set --base-url to the actual published HTTPS release directory. No official download host is configured yet.' >&2; exit 1;; esac
case "$VERSION" in ''|*[!0-9A-Za-z._-]*) echo 'Invalid version' >&2;exit 1;; esac
case "$ROLE" in both|hub) ;; node) [ -n "$HUB" ] && [ -n "$CODE" ] || { echo 'Node installation requires --hub and --code from your control console.' >&2;exit 1;};; *) exit 1;; esac
case "$(uname -s)" in Darwin) OS=darwin;; Linux) OS=linux;; *) echo 'Supported: macOS and Linux.' >&2;exit 1;; esac
case "$(uname -m)" in arm64|aarch64) ARCH=arm64;; x86_64|amd64) ARCH=amd64;; *) echo '64-bit ARM or x86 required; 32-bit Raspberry Pi OS is not supported.' >&2;exit 1;; esac
if [ "$OS-$ARCH" = darwin-amd64 ]; then echo 'Intel Mac release is not currently available.' >&2;exit 1;fi
command -v curl >/dev/null
command -v tar >/dev/null
TMPDIR_INSTALL=$(mktemp -d)
trap 'rm -rf "$TMPDIR_INSTALL"' EXIT HUP INT TERM
NAME="pocket-agents-$VERSION-$OS-$ARCH"
URL=${BASE%/}
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' "$URL/$NAME.tar.gz" -o "$TMPDIR_INSTALL/package.tar.gz"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' "$URL/SHA256SUMS" -o "$TMPDIR_INSTALL/SHA256SUMS"
EXPECTED=$(awk -v name="$NAME.tar.gz" '$2==name {print $1}' "$TMPDIR_INSTALL/SHA256SUMS")
[ "${#EXPECTED}" -eq 64 ] || { echo 'Missing or ambiguous release checksum.' >&2;exit 1; }
if command -v sha256sum >/dev/null; then ACTUAL=$(sha256sum "$TMPDIR_INSTALL/package.tar.gz" | awk '{print $1}'); else ACTUAL=$(shasum -a 256 "$TMPDIR_INSTALL/package.tar.gz" | awk '{print $1}'); fi
[ "$ACTUAL" = "$EXPECTED" ] || { echo 'Checksum mismatch; nothing installed.' >&2;exit 1; }
# Extract only the expected binary, never arbitrary archive paths.
tar -xOf "$TMPDIR_INSTALL/package.tar.gz" "$NAME/pocket" > "$TMPDIR_INSTALL/pocket"
chmod 700 "$TMPDIR_INSTALL/pocket"
if [ "$OS" = darwin ]; then
 /usr/bin/codesign --verify --strict "$TMPDIR_INSTALL/pocket"
 /usr/sbin/spctl --assess --type execute "$TMPDIR_INSTALL/pocket" || { echo 'macOS release is not trusted/notarized. Publisher must complete Developer ID signing. Do not disable Gatekeeper.' >&2;exit 1; }
else
 systemctl --user show-environment >/dev/null 2>&1 || { echo 'A user systemd session is required. On a headless server, configure a user service session/linger with your administrator, or run pocket in foreground.' >&2;exit 1; }
fi
if [ "$ROLE" = node ]; then
 "$TMPDIR_INSTALL/pocket" pair --hub "$HUB" --code "$CODE"
 "$TMPDIR_INSTALL/pocket" service install --role node
 "$HOME/.local/bin/pocket" service restart || "$HOME/.local/bin/pocket" service start
 echo 'Execution device connected. Return to your control console to verify CLI login and projects.'
else
 if [ "$OPEN" -eq 1 ]; then "$TMPDIR_INSTALL/pocket" setup --role "$ROLE"; else
  "$TMPDIR_INSTALL/pocket" service install --role "$ROLE"
  "$HOME/.local/bin/pocket" service restart || "$HOME/.local/bin/pocket" service start
  echo 'Installed. Run ~/.local/bin/pocket open to open the setup page.'
 fi
fi
printf '\nInstalled to %s/.local/bin/pocket. Add ~/.local/bin to PATH if needed.\n' "$HOME"
