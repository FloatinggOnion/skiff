#!/usr/bin/env bash
# Runs the Skiff GitHub Action. Everything comes from SKIFF_* variables, set by action.yml.
set -uo pipefail

fail() {
  echo "::error::$(printf '%s' "$1" | tr '\r\n' '  ')"
  exit 1
}

command -v curl >/dev/null || fail "This action needs curl on the runner."
command -v jq >/dev/null || fail "This action needs jq on the runner."

server="${SKIFF_SERVER%/}"
[ -n "$server" ] || fail "Set the server input to your Skiff dashboard address."
[ -n "${SKIFF_TOKEN:-}" ] || fail "Set the token input. Create an API token in Skiff under Settings, and keep it in a secret."
[ -n "${SKIFF_APP:-}" ] || fail "Set the app input to the project's name in Skiff."

command="${SKIFF_COMMAND:-deploy}"
timeout="${SKIFF_TIMEOUT:-900}"
poll="${SKIFF_POLL:-5}"
app_path="$(jq -rn --arg v "$SKIFF_APP" '$v|@uri')"
body_file="$(mktemp)"
trap 'rm -f "$body_file"' EXIT

# api METHOD PATH [JSON] sets $code and leaves the response in $body_file. A code of 000 means the server couldn't be reached.
api() {
  local args=(-sS -o "$body_file" -w '%{http_code}' -X "$1" --max-time 60 -H "Authorization: Bearer $SKIFF_TOKEN")
  if [ $# -ge 3 ]; then
    args+=(-H 'Content-Type: application/json' -d "$3")
  fi
  code="$(curl "${args[@]}" "$server/api/v1$2" 2>/dev/null)" || true
  [ -n "$code" ] || code=000
}

api_error() {
  local detail
  detail="$(jq -r '.error // empty' "$body_file" 2>/dev/null || true)"
  case "$code" in
    000) fail "$1: couldn't reach $server." ;;
    401) fail "$1: Skiff rejected the API token." ;;
    *) fail "$1: ${detail:-the server answered $code}" ;;
  esac
}

output() {
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    printf '%s=%s\n' "$1" "$(printf '%s' "$2" | tr -d '\r\n')" >>"$GITHUB_OUTPUT"
  fi
}

branch="${SKIFF_BRANCH:-}"
if [ -z "$branch" ]; then
  branch="${GITHUB_HEAD_REF:-${GITHUB_REF_NAME:-}}"
fi

case "$command" in
deploy)
  if [ -n "${SKIFF_IMAGE:-}" ]; then
    payload="$(jq -n --arg image "$SKIFF_IMAGE" '{image: $image}')"
    api POST "/apps/$app_path/deploy" "$payload"
  else
    api POST "/apps/$app_path/deploy"
  fi
  [ "$code" = 202 ] || api_error "Couldn't start the deploy"
  ;;
preview)
  [ -n "$branch" ] || fail "Set the branch input."
  payload="$(jq -n --arg branch "$branch" '{branch: $branch}')"
  api POST "/apps/$app_path/previews" "$payload"
  [ "$code" = 202 ] || api_error "Couldn't start the preview"
  ;;
remove-preview)
  [ -n "$branch" ] || fail "Set the branch input."
  branch_query="$(jq -rn --arg v "$branch" '$v|@uri')"
  api DELETE "/apps/$app_path/previews?branch=$branch_query"
  case "$code" in
  204) echo "Removed the preview of $branch." ;;
  404) echo "There's no preview of $branch to remove." ;;
  *) api_error "Couldn't remove the preview" ;;
  esac
  exit 0
  ;;
*)
  fail "Unknown command '$command'. Use deploy, preview or remove-preview."
  ;;
esac

deploy_id="$(jq -r '.id' "$body_file" | tr -d '\r\n')"
target="$(jq -r '.app' "$body_file" | tr -d '\r\n')"
url="$(jq -r '.url // empty' "$body_file")"
output deploy-id "$deploy_id"
output app "$target"
output url "$url"
echo "Deploying $target ($deploy_id)"

deadline=$((SECONDS + timeout))
status=building
while :; do
  api GET "/deploys/$deploy_id"
  # The server may restart during its own update, so a failed check isn't a failed deploy.
  if [ "$code" = 200 ]; then
    status="$(jq -r '.status' "$body_file")"
    commit="$(jq -r '.commit // empty' "$body_file")"
    [ "$status" = building ] || break
  elif [ "$code" = 401 ] || [ "$code" = 404 ]; then
    api_error "Couldn't follow the deploy"
  fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    fail "$target is still deploying after ${timeout}s. See its progress in the Skiff dashboard. Raise the timeout input if it needs longer."
  fi
  sleep "$poll"
done

output commit "${commit:-}"
if [ "$status" != live ]; then
  api GET "/deploys/$deploy_id/log"
  if [ "$code" = 200 ]; then
    stop="$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
    echo "::group::Build log"
    echo "::stop-commands::$stop"
    jq -r '.log' "$body_file"
    echo "::$stop::"
    echo "::endgroup::"
  fi
  fail "The deploy of $target ended as $status."
fi

echo "$target is live${url:+ at $url}"
if [ -n "${commit:-}" ] && [ -n "${GITHUB_SHA:-}" ] && [ "$command" = deploy ] && [ "${GITHUB_SHA#"$commit"}" = "$GITHUB_SHA" ]; then
  commit="$(printf '%s' "$commit" | tr -cd 'A-Za-z0-9')"
  echo "::notice::Skiff deployed $commit, the newest commit on the branch, not ${GITHUB_SHA:0:7}, which this workflow ran on."
fi
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  echo "**$target** is live${url:+ at $url}" >>"$GITHUB_STEP_SUMMARY"
fi
