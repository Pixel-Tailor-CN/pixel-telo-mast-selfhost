#!/usr/bin/env bash
set -euo pipefail

readonly upstream_git_url="https://github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost.git"

fetch_version_history() {
  if git remote get-url origin >/dev/null 2>&1; then
    if [[ "$(git rev-parse --is-shallow-repository 2>/dev/null || true)" == "true" ]]; then
      git fetch --quiet --tags --unshallow origin 2>/dev/null || \
        git fetch --quiet --tags --deepen=1000 origin 2>/dev/null || true
    else
      git fetch --quiet --tags origin 2>/dev/null || true
    fi
  fi

  git fetch --quiet --force "${upstream_git_url}" \
    '+refs/tags/v*:refs/mast-release-tags/v*' 2>/dev/null || true
}

list_release_tags() {
  local ref
  local tag
  local found=false

  while IFS= read -r ref; do
    tag="${ref#refs/mast-release-tags/}"
    if [[ "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      printf '%s\t%s\n' "${tag}" "${ref}"
      found=true
    fi
  done < <(git for-each-ref --sort=-version:refname --format='%(refname)' refs/mast-release-tags/)

  if [[ "${found}" == "false" ]]; then
    while IFS= read -r ref; do
      tag="${ref#refs/tags/}"
      if [[ "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        printf '%s\t%s\n' "${tag}" "${ref}"
      fi
    done < <(git for-each-ref --sort=-version:refname --format='%(refname)' refs/tags/)
  fi
}

resolve_version() {
  local commit_sha="$1"
  local short_sha="${commit_sha:0:7}"
  local tag
  local ref
  local tag_commit
  local base_tag=""

  while IFS=$'\t' read -r tag ref; do
    [[ -n "${tag}" && -n "${ref}" ]] || continue
    tag_commit="$(git rev-parse "${ref}^{commit}" 2>/dev/null || true)"
    [[ -n "${tag_commit}" ]] || continue
    if [[ "${tag_commit}" == "${commit_sha}" ]]; then
      printf '%s\n' "${tag#v}"
      return
    fi
    if [[ -z "${base_tag}" ]] && git merge-base --is-ancestor "${tag_commit}" "${commit_sha}" 2>/dev/null; then
      base_tag="${tag}"
    fi
  done < <(list_release_tags)

  if [[ -n "${base_tag}" ]]; then
    printf '%s-dev+%s\n' "${base_tag#v}" "${short_sha}"
    return
  fi
  printf 'dev+%s\n' "${short_sha}"
}

main() {
  local commit_sha
  local version
  local output_file

  commit_sha="${VERCEL_GIT_COMMIT_SHA:-$(git rev-parse HEAD)}"
  if [[ ! "${commit_sha}" =~ ^[0-9a-fA-F]{7,64}$ ]]; then
    printf 'invalid deployment commit SHA\n' >&2
    exit 1
  fi

  fetch_version_history
  version="$(resolve_version "${commit_sha}")"
  output_file="${VERCEL_OUTPUT_FILE:-server}"
  mkdir -p "$(dirname "${output_file}")"

  printf 'building Vercel deployment version=%s commit=%s\n' "${version}" "${commit_sha}"
  go build -trimpath \
    -ldflags "-s -w -X main.version=${version} -X main.commit=${commit_sha}" \
    -o "${output_file}" ./cmd/api
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
