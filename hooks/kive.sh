#!/usr/bin/env bash
# Copyright 2025 Kiruba Sankar Swaminathan. All rights reserved.
# Use of this source code is governed by the GNU AGPL v3
# license that can be found in the LICENSE file.
#
# Client helpers for the kive hook runtime API (KV store, demands, semaphores).
# Source from a hook script: source ./kive.sh

: "${HOOK_API_HOST:=127.0.0.1}"
: "${HOOK_API_PORT:=8080}"
KIVE_ROUTE_STORE_KEYS="/kv"
KIVE_ROUTE_STORE_KEYS_LIST="/kv/keys"
KIVE_ROUTE_STORE_SECRET="/kv/secret"
KIVE_ROUTE_DEMANDS="/demands"
KIVE_ROUTE_SEMAPHORE_ACQUIRE="/semaphore/acquire"
KIVE_ROUTE_SEMAPHORE_RELEASE="/semaphore/release"
KIVE_ROUTE_SEMAPHORE_STATUS="/semaphore/status"
KIVE_ROUTE_HTTP="/http"

kive_runtime_api_base_url() {
  echo "http://${HOOK_API_HOST}:${HOOK_API_PORT}"
}

# curl with kive headers. Extra args are passed to curl (method, URL, -d, etc.).
kive_curl() {
  local -a auth_args=()
  if [[ -n "${HOOK_API_TOKEN:-}" ]]; then
    auth_args=(-H "Authorization: Bearer ${HOOK_API_TOKEN}")
  fi
  curl -sS \
    -H "X-ALLOCATION-ID: ${ALLOCATION_ID:-}" \
    -H "HOOK: ${HOOK:-}" \
    -H "EVENT: ${EVENT:-}" \
    -H "Content-Type: application/json" \
    -H "Accept: application/json" \
    "${auth_args[@]}" \
    "$@"
}

allocation_id() { echo "${ALLOCATION_ID:-}"; }
allocation_ip() { echo "${ALLOCATION_IP:-}"; }
allocation_index() { echo "${ALLOCATION_INDEX:-}"; }

is_allocation_disabled() {
  [[ "${DISABLED:-}" == "1" ]]
}

# True on the first allocation of the first batch.
# Prefer BATCH_ALLOCATIONS[0] over ALLOCATION_INDEX=0: rollout order can put
# catalog index 0 in a later batch, which would otherwise make no worker one-shot.
is_one_shot() {
  [[ "${BATCH_INDEX:-0}" == "0" ]] || return 1
  if [[ -n "${BATCH_ALLOCATIONS:-}" ]]; then
    local first
    first=$(printf '%s' "${BATCH_ALLOCATIONS}" | cut -d, -f1)
    first="${first#"${first%%[![:space:]]*}"}"
    first="${first%"${first##*[![:space:]]}"}"
    [[ "${ALLOCATION_IP:-}" == "$first" ]]
  else
    [[ "${ALLOCATION_INDEX:-0}" == "0" ]]
  fi
}

hook_event() { echo "${EVENT:-}"; }
hook_name() { echo "${HOOK:-}"; }
job_name() { echo "${JOB:-}"; }

get_store_value() {
  local namespace="$1"
  local key="$2"
  local body
  body=$(printf '{"namespace":%s,"key":%s}' "$(kive_json_string "$namespace")" "$(kive_json_string "$key")")
  kive_curl -X GET "$(kive_runtime_api_base_url)${KIVE_ROUTE_STORE_KEYS}" -d "$body"
}

put_rollout_order() {
  local order="$1"
  local body
  body=$(printf '{"namespace":%s,"key":"rollout_order","value":%s}' \
    "$(kive_json_string "kive/job/$(job_name)")" \
    "$(kive_json_string "$order")")
  kive_curl -X PUT "$(kive_runtime_api_base_url)${KIVE_ROUTE_STORE_KEYS}" -d "$body"
}

get_rollout_order() {
  get_store_value "kive/job/$(job_name)" "rollout_order"
}

put_job_variable() {
  local key="$1"
  local value="$2"
  local ttl="${3:-0}"
  local body
  if [[ "$ttl" != "0" && -n "$ttl" ]]; then
    body=$(printf '{"namespace":%s,"key":%s,"value":%s,"ttl":%s}' \
      "$(kive_json_string "vars/job/$(job_name)")" \
      "$(kive_json_string "$key")" \
      "$(kive_json_string "$value")" \
      "$ttl")
  else
    body=$(printf '{"namespace":%s,"key":%s,"value":%s}' \
      "$(kive_json_string "vars/job/$(job_name)")" \
      "$(kive_json_string "$key")" \
      "$(kive_json_string "$value")")
  fi
  kive_curl -X PUT "$(kive_runtime_api_base_url)${KIVE_ROUTE_STORE_KEYS}" -d "$body"
}

put_job_secret() {
  local key="$1"
  local value="$2"
  local ttl="${3:-0}"
  local body
  if [[ "$ttl" != "0" && -n "$ttl" ]]; then
    body=$(printf '{"namespace":%s,"key":%s,"value":%s,"ttl":%s}' \
      "$(kive_json_string "secrets/job/$(job_name)")" \
      "$(kive_json_string "$key")" \
      "$(kive_json_string "$value")" \
      "$ttl")
  else
    body=$(printf '{"namespace":%s,"key":%s,"value":%s}' \
      "$(kive_json_string "secrets/job/$(job_name)")" \
      "$(kive_json_string "$key")" \
      "$(kive_json_string "$value")")
  fi
  kive_curl -X PUT "$(kive_runtime_api_base_url)${KIVE_ROUTE_STORE_SECRET}" -d "$body"
}

list_job_keys() {
  local namespace="${1:-}"
  local body="{}"
  if [[ -n "$namespace" ]]; then
    body=$(printf '{"namespace":%s}' "$(kive_json_string "$namespace")")
  fi
  kive_curl -X GET "$(kive_runtime_api_base_url)${KIVE_ROUTE_STORE_KEYS_LIST}" -d "$body"
}

delete_job_variable() {
  local key="$1"
  local body
  body=$(printf '{"namespace":%s,"key":%s}' \
    "$(kive_json_string "vars/job/$(job_name)")" \
    "$(kive_json_string "$key")")
  kive_curl -X DELETE "$(kive_runtime_api_base_url)${KIVE_ROUTE_STORE_KEYS}" -d "$body"
}

delete_job_secret() {
  local key="$1"
  local body
  body=$(printf '{"namespace":%s,"key":%s}' \
    "$(kive_json_string "secrets/job/$(job_name)")" \
    "$(kive_json_string "$key")")
  kive_curl -X DELETE "$(kive_runtime_api_base_url)${KIVE_ROUTE_STORE_SECRET}" -d "$body"
}

list_hook_demands() {
  kive_curl "$(kive_runtime_api_base_url)${KIVE_ROUTE_DEMANDS}"
}

acquire_semaphore() {
  local name="$1"
  local capacity="${2:-1}"
  local timeout_seconds="${3:-600}"
  local body
  body=$(printf '{"name":%s,"capacity":%s,"timeout_seconds":%s}' \
    "$(kive_json_string "$name")" "$capacity" "$timeout_seconds")
  kive_curl --max-time $((timeout_seconds + 30)) -X POST \
    "$(kive_runtime_api_base_url)${KIVE_ROUTE_SEMAPHORE_ACQUIRE}" -d "$body"
}

release_semaphore() {
  local name="$1"
  local body
  body=$(printf '{"name":%s}' "$(kive_json_string "$name")")
  kive_curl -X POST "$(kive_runtime_api_base_url)${KIVE_ROUTE_SEMAPHORE_RELEASE}" -d "$body"
}

semaphore_status() {
  local name="$1"
  kive_curl "$(kive_runtime_api_base_url)${KIVE_ROUTE_SEMAPHORE_STATUS}?name=$(kive_urlencode "$name")"
}

# Outbound HTTP(S) via POST /http. Args: method url [json-headers] [body] [timeout_seconds] [json-tls]
http_request() {
  local method="$1"
  local url="$2"
  local headers_json="${3:-{}}"
  local body="${4:-}"
  local timeout_seconds="${5:-30}"
  local tls_json="${6:-{}}"
  local payload
  payload=$(printf '{"method":%s,"url":%s,"headers":%s,"body":%s,"timeout_seconds":%s,"tls":%s}' \
    "$(kive_json_string "$method")" \
    "$(kive_json_string "$url")" \
    "$headers_json" \
    "$(kive_json_string "$body")" \
    "$timeout_seconds" \
    "$tls_json")
  kive_curl --max-time $((timeout_seconds + 30)) -X POST \
    "$(kive_runtime_api_base_url)${KIVE_ROUTE_HTTP}" -d "$payload"
}

# Encode a string as a JSON string literal.
kive_json_string() {
  local s="$1"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$s"
    return
  fi
  printf '"%s"' "${s//\\/\\\\}"
}

kive_urlencode() {
  local s="$1"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))' "$s"
    return
  fi
  printf '%s' "$s"
}

# Legacy aliases
get_allocation_id() { allocation_id; }
get_allocation_ip() { allocation_ip; }
get_allocation_index() { allocation_index; }
get_event() { hook_event; }
get_command() { hook_name; }
get_job() { job_name; }
kv_get() { get_store_value "$@"; }
kv_put() { put_job_variable "$@"; }
kv_put_secret() { put_job_secret "$@"; }
get_demands() { list_hook_demands; }
