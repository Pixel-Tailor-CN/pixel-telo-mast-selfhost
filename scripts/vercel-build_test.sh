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

official_tag() {
  local dir="$1"
  local tag="$2"
  local sha="$3"
  git -C "${dir}" update-ref "refs/mast-release-tags/${tag}" "${sha}"
}

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

tagged_repo="${tmp}/tagged"
new_repo "${tagged_repo}"
release_sha="$(commit_file "${tagged_repo}" release)"
official_tag "${tagged_repo}" v0.2.0 "${release_sha}"
assert_equal "0.2.0" "$(cd "${tagged_repo}" && resolve_version "${release_sha}")" "exact release tag"

dev_sha="$(commit_file "${tagged_repo}" development)"
assert_equal "0.2.0-dev+${dev_sha:0:7}" "$(cd "${tagged_repo}" && resolve_version "${dev_sha}")" "commit after release"

nearest_repo="${tmp}/nearest"
new_repo "${nearest_repo}"
far_sha="$(commit_file "${nearest_repo}" far)"
official_tag "${nearest_repo}" v2.0.0 "${far_sha}"
near_sha="$(commit_file "${nearest_repo}" near)"
official_tag "${nearest_repo}" v1.9.9 "${near_sha}"
head_sha="$(commit_file "${nearest_repo}" head)"
assert_equal "1.9.9-dev+${head_sha:0:7}" "$(cd "${nearest_repo}" && resolve_version "${head_sha}")" "nearest release tag"

invalid_sha="$(commit_file "${nearest_repo}" invalid-tag)"
official_tag "${nearest_repo}" v01.2.3 "${invalid_sha}"
assert_equal "1.9.9-dev+${invalid_sha:0:7}" "$(cd "${nearest_repo}" && resolve_version "${invalid_sha}")" "reject leading zero tag"

untagged_repo="${tmp}/untagged"
new_repo "${untagged_repo}"
untagged_sha="$(commit_file "${untagged_repo}" initial)"
git -C "${untagged_repo}" tag v9.9.9 "${untagged_sha}"
assert_equal "dev+${untagged_sha:0:7}" "$(cd "${untagged_repo}" && resolve_version "${untagged_sha}")" "ignore user repository tag"

official_repo="${tmp}/official"
new_repo "${official_repo}"
official_sha="$(commit_file "${official_repo}" official-release)"
git -C "${official_repo}" tag v3.0.0 "${official_sha}"
official_bare="${tmp}/official.git"
git clone -q --bare "${official_repo}" "${official_bare}"
user_bare="${tmp}/user.git"
git clone -q --bare "${official_repo}" "${user_bare}"
git --git-dir="${user_bare}" tag -d v3.0.0 >/dev/null
user_clone="${tmp}/user-clone"
git clone -q --depth 1 "file://${user_bare}" "${user_clone}"
git -C "${user_clone}" tag v9.9.9
upstream_git_url="file://${official_bare}"
user_head="$(git -C "${user_clone}" rev-parse HEAD)"
assert_equal "3.0.0" "$(cd "${user_clone}" && fetch_version_history >/dev/null 2>&1; resolve_version "${user_head}")" "fetch official tags into isolated namespace"

printf 'PASS: vercel build version resolution\n'
