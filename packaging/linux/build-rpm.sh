#!/usr/bin/env bash
# 在已生成 bundle/ 后构建 dagents-backend RPM（与 build-deb.sh 安装布局一致）。
set -euo pipefail

VERSION="${1:-0.0.0}"
DEB_ARCH="${2:-amd64}"
OUTPUT_NAME="${3:-dagents-backend-linux-${DEB_ARCH}.rpm}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
BUNDLE_DIR="$ROOT_DIR/bundle"
RPM_TOP="$ROOT_DIR/rpm-build"
SPEC_TEMPLATE="$ROOT_DIR/packaging/linux/dagents-backend.spec"
SOURCE_DIR="$RPM_TOP/SOURCES/dagents-backend-$VERSION"

if ! command -v rpmbuild >/dev/null 2>&1; then
  echo "[build-rpm] rpmbuild not found; install rpm package (e.g. apt install rpm)" >&2
  exit 1
fi

if [[ ! -d "$BUNDLE_DIR" ]]; then
  echo "[build-rpm] missing bundle directory: $BUNDLE_DIR" >&2
  exit 1
fi
if [[ ! -f "$BUNDLE_DIR/dagents-api" ]]; then
  echo "[build-rpm] missing bundle/dagents-api" >&2
  exit 1
fi
if [[ ! -f "$BUNDLE_DIR/dagents_register_center" ]]; then
  echo "[build-rpm] missing bundle/dagents_register_center" >&2
  exit 1
fi
if [[ ! -f "$BUNDLE_DIR/dagents-cli" ]]; then
  echo "[build-rpm] missing bundle/dagents-cli" >&2
  exit 1
fi

case "$DEB_ARCH" in
  amd64) RPM_ARCH="x86_64" ;;
  i386) RPM_ARCH="i686" ;;
  *)
    echo "[build-rpm] unsupported arch for RPM: $DEB_ARCH (use amd64 or i386)" >&2
    exit 1
    ;;
esac

rm -rf "$RPM_TOP"
mkdir -p "$RPM_TOP"/{BUILD,RPMS,SOURCES,SPECS,SRPMS,BUILDROOT}
mkdir -p "$SOURCE_DIR/bundle"
cp -a "$BUNDLE_DIR/." "$SOURCE_DIR/bundle/"
install -m 0755 "$ROOT_DIR/packaging/linux/dagents" "$SOURCE_DIR/dagents"

SPEC_FILE="$RPM_TOP/SPECS/dagents-backend.spec"
sed -e "s/@VERSION@/$VERSION/g" -e "s/@BUILDARCH@/$RPM_ARCH/g" "$SPEC_TEMPLATE" > "$SPEC_FILE"

rpmbuild -bb \
  --define "_topdir $RPM_TOP" \
  --target "$RPM_ARCH" \
  "$SPEC_FILE"

shopt -s nullglob
built_candidates=("$RPM_TOP/RPMS/$RPM_ARCH"/dagents-backend-"${VERSION}"-*.rpm)
shopt -u nullglob
if [[ ${#built_candidates[@]} -eq 0 ]]; then
  echo "[build-rpm] no RPM produced under $RPM_TOP/RPMS/$RPM_ARCH" >&2
  ls -la "$RPM_TOP/RPMS/$RPM_ARCH" 2>/dev/null || true
  exit 1
fi
BUILT_RPM="${built_candidates[0]}"

cp -f "$BUILT_RPM" "$ROOT_DIR/$OUTPUT_NAME"
echo "[build-rpm] wrote $ROOT_DIR/$OUTPUT_NAME"
