#!/usr/bin/env bash
set -euo pipefail

VERSION="${1:-0.0.0}"
ARCH="${2:-amd64}"
OUTPUT_NAME="${3:-dagents-backend-linux-${ARCH}.deb}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
BUNDLE_DIR="$ROOT_DIR/bundle"
PKG_DIR="$ROOT_DIR/deb-root"
INSTALL_DIR="$PKG_DIR/opt/dagents/backend"
BIN_DIR="$PKG_DIR/usr/bin"
DEBIAN_DIR="$PKG_DIR/DEBIAN"

if [[ ! -d "$BUNDLE_DIR" ]]; then
  echo "[build-deb] missing bundle directory: $BUNDLE_DIR" >&2
  exit 1
fi
if [[ ! -f "$BUNDLE_DIR/dagents-api" ]]; then
  echo "[build-deb] missing bundle/dagents-api" >&2
  exit 1
fi
if [[ ! -f "$BUNDLE_DIR/dagents_register_center" ]]; then
  echo "[build-deb] missing bundle/dagents_register_center" >&2
  exit 1
fi
if [[ ! -f "$BUNDLE_DIR/dagents-cli" ]]; then
  echo "[build-deb] missing bundle/dagents-cli" >&2
  exit 1
fi

rm -rf "$PKG_DIR"
mkdir -p "$INSTALL_DIR" "$BIN_DIR" "$DEBIAN_DIR"
cp -a "$BUNDLE_DIR/." "$INSTALL_DIR/"
install -m 0755 "$ROOT_DIR/packaging/linux/dagents" "$BIN_DIR/dagents"
chmod 0755 "$INSTALL_DIR/dagents-api" "$INSTALL_DIR/dagents_register_center" "$INSTALL_DIR/dagents-cli" || true

installed_size=$(du -sk "$PKG_DIR" | awk '{print $1}')
cat > "$DEBIAN_DIR/control" <<EOF
Package: dagents-backend
Version: $VERSION
Section: utils
Priority: optional
Architecture: $ARCH
Maintainer: DAgents <noreply@example.com>
Installed-Size: $installed_size
Description: DAgents backend runtime
 Installs the DAgents backend runtime and exposes the dagents command.
EOF

cat > "$DEBIAN_DIR/postinst" <<'EOF'
#!/usr/bin/env sh
set -e
chmod 0755 /usr/bin/dagents || true
chmod 0755 /opt/dagents/backend/dagents-api /opt/dagents/backend/dagents_register_center /opt/dagents/backend/dagents-cli || true
exit 0
EOF
chmod 0755 "$DEBIAN_DIR/postinst"

dpkg-deb --build "$PKG_DIR" "$ROOT_DIR/$OUTPUT_NAME"
echo "[build-deb] wrote $ROOT_DIR/$OUTPUT_NAME"
