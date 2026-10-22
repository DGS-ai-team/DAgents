#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp_root="$(mktemp -d)"
trap 'rm -rf "${tmp_root}"' EXIT

mkdir -p \
  "${tmp_root}/scripts/ci" \
  "${tmp_root}/packaging/manage/scripts" \
  "${tmp_root}/dist"
cp "${repo_root}/scripts/ci/assemble_manage_bundle.sh" "${tmp_root}/scripts/ci/"
cp "${repo_root}/packaging/manage/docker-compose.offline.yml" "${tmp_root}/packaging/manage/"
cp "${repo_root}/packaging/manage/.env.example" "${tmp_root}/packaging/manage/"
cp "${repo_root}/packaging/manage/scripts/import-image.sh" "${tmp_root}/packaging/manage/scripts/"
cp "${repo_root}/packaging/manage/scripts/restart.sh" "${tmp_root}/packaging/manage/scripts/"
cp "${repo_root}/packaging/manage/scripts/import-image.bat" "${tmp_root}/packaging/manage/scripts/"
cp "${repo_root}/packaging/manage/scripts/restart.bat" "${tmp_root}/packaging/manage/scripts/"

version="0.0.0-ci"
product_prefix="da""gents"
printf 'test image' > "${tmp_root}/dist/${product_prefix}-manage-${version}.tar.gz"
printf 'test release, already seeded in the image' > "${tmp_root}/dist/${product_prefix}-local-assistant-linux-amd64-${version}.tar.gz"

(
  cd "${tmp_root}"
  VERSION="${version}" SKIP_BUILD=1 bash scripts/ci/assemble_manage_bundle.sh >/dev/null
)

archive="${tmp_root}/dist/${product_prefix}-manage-bundle-${version}.tar.gz"
members="${tmp_root}/members.txt"
tar -tzf "${archive}" > "${members}"
grep -Fxq "${product_prefix}-manage-bundle-${version}/image/${product_prefix}-manage-${version}.tar.gz" "${members}"
grep -Fxq "${product_prefix}-manage-bundle-${version}/VERSION" "${members}"
grep -Fxq "${product_prefix}-manage-bundle-${version}/docker-compose.yml" "${members}"
if grep -q '^da[^/]*/releases/' "${members}"; then
  echo "Manage bundle must not duplicate the release already seeded by its image" >&2
  exit 1
fi
echo "Manage bundle contains the image and setup files without a duplicate release archive."
