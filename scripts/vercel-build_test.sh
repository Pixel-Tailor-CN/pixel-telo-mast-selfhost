#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=vercel-build.sh
source "${repo_root}/scripts/vercel-build.sh"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_equal() {
  local want="$1"
  local got="$2"
  local name="$3"
  [[ "${got}" == "${want}" ]] || fail "${name}: want ${want}, got ${got}"
}

new_repo() {
  local dir="$1"
  git init -q "${dir}"
  git -C "${dir}" config user.name test
  git -C "${dir}" config user.email test@example.com
}

commit_file() {
  local dir="$1"
  local content="$2"
  printf '%s\n' "${content}" >"${dir}/state.txt"
  git -C "${dir}" add state.txt
  git -C "${dir}" commit -q -m "${content}"
  git -C "${dir}" rev-parse HEAD
}

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

tagged_repo="${tmp}/tagged"
new_repo "${tagged_repo}"
release_sha="$(commit_file "${tagged_repo}" release)"
git -C "${tagged_repo}" tag v0.2.0 "${release_sha}"
assert_equal "0.2.0" "$(cd "${tagged_repo}" && resolve_version "${release_sha}")" "exact release tag"

dev_sha="$(commit_file "${tagged_repo}" development)"
assert_equal "0.2.0-dev+${dev_sha:0:7}" "$(cd "${tagged_repo}" && resolve_version "${dev_sha}")" "commit after release"

untagged_repo="${tmp}/untagged"
new_repo "${untagged_repo}"
untagged_sha="$(commit_file "${untagged_repo}" initial)"
assert_equal "dev+${untagged_sha:0:7}" "$(cd "${untagged_repo}" && resolve_version "${untagged_sha}")" "repository without release tag"

printf 'PASS: vercel build version resolution\n'
