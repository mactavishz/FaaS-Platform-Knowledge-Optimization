#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BENCHMARK_DIR="$ROOT_DIR/benchmark"
TERRAFORM_DIR="$ROOT_DIR/terraform"
ENV_DIR="$BENCHMARK_DIR/env"
WORKFLOWS_DIR="$ROOT_DIR/tests/workflows"

RUN_TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_NAME="${RUN_NAME:-$RUN_TIMESTAMP}"
PROFILES="${PROFILES:-}"
PLATFORMS="${PLATFORMS:-faasd tinyfaas}"
WORKFLOWS="${WORKFLOWS:-iot tree webshop}"
WORKFLOW_CPU_LIMIT=${WORKFLOW_CPU_LIMIT:-0.5}
WORKFLOW_MEMORY_LIMIT=${WORKFLOW_MEMORY_LIMIT:-1024Mi}
OUTPUT_ROOT="${OUTPUT_ROOT:-$BENCHMARK_DIR/results/$RUN_NAME}"
# n2-standard-8: 8 vCPUs, 32 GB memory
MACHINE_TYPE="${MACHINE_TYPE:-n2-standard-8}"
SSH_PRIVATE_KEY="${SSH_PRIVATE_KEY:-$TERRAFORM_DIR/gcp}"
SSH_PUBLIC_KEY="${SSH_PUBLIC_KEY:-$TERRAFORM_DIR/gcp.pub}"
SSH_USER="${SSH_USER:-bench}"
K6_ITERATIONS="${K6_ITERATIONS:-70}"
K6_VUS="${K6_VUS:-1}"
K6_MAX_DURATION="${K6_MAX_DURATION:-6h}"
K6_GRACEFUL_STOP="${K6_GRACEFUL_STOP:-30s}"
CONTINUE_ON_ERROR="${CONTINUE_ON_ERROR:-false}"
KEEP_INFRA_ON_FAILURE="${KEEP_INFRA_ON_FAILURE:-false}"
RERUN_OVERWRITE="${RERUN_OVERWRITE:-false}"
DRY_RUN="${DRY_RUN:-false}"

INTERRUPTED="false"
EXIT_STATUS=0
RESOLVED_PROFILES=()
RUN_PIDS=()
RUN_PID_PLATFORMS=()
CHILD_PLATFORM=""
CHILD_PROFILE_PATH=""
CHILD_RUN_DIR=""
CHILD_INFRA_ACTIVE="false"
CHILD_CLEANUP_DONE="false"

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2
}

fatal() {
  log "ERROR: $*"
  exit 1
}

bool_true() {
  case "$(lowercase "$1")" in
    1 | true | yes | y) return 0 ;;
    *) return 1 ;;
  esac
}

lowercase() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]'
}

ensure_dependencies() {
  local commands=(terraform jq curl k6 faas-cli ssh scp)
  for cmd in "${commands[@]}"; do
    command -v "$cmd" >/dev/null 2>&1 || fatal "required command not found: $cmd"
  done
}

abs_path() {
  local path="$1"
  if [[ "$path" = /* ]]; then
    printf '%s\n' "$path"
  else
    printf '%s\n' "$ROOT_DIR/$path"
  fi
}

get_profile_path() {
  local profile="$1"
  if [[ "$profile" = */* || "$profile" = *.env ]]; then
    abs_path "$profile"
  else
    printf '%s\n' "$ENV_DIR/$profile.env"
  fi
}

get_profile_name() {
  local path="$1"
  basename "$path" .env
}

get_workflow_dir_name() {
  case "$(lowercase "$1")" in
    iot) printf 'IoT\n' ;;
    tree) printf 'tree\n' ;;
    webshop) printf 'webshop\n' ;;
    linear3) fatal "linear3 is intentionally excluded from benchmark runs" ;;
    *) fatal "unsupported workflow: $1" ;;
  esac
}

get_k6_workflow_name() {
  case "$(lowercase "$1")" in
    iot) printf 'iot\n' ;;
    tree) printf 'tree\n' ;;
    webshop) printf 'webshop\n' ;;
    *) fatal "unsupported workflow: $1" ;;
  esac
}

get_workflow_k6_script() {
  case "$(lowercase "$1")" in
    iot | tree) printf '%s\n' "$BENCHMARK_DIR/scripts/workflow_cold_latency.js" ;;
    webshop) printf '%s\n' "$BENCHMARK_DIR/scripts/webshop_user_journey.js" ;;
    *) fatal "unsupported workflow: $1" ;;
  esac
}

get_stack_file_path() {
  local platform="$1"
  local workflow="$2"
  local workflow_dir
  workflow_dir="$(get_workflow_dir_name "$workflow")"
  printf '%s\n' "$WORKFLOWS_DIR/$platform/$workflow_dir/stack.yaml"
}

resolve_profiles() {
  if [[ -n "$PROFILES" ]]; then
    local profile
    for profile in $PROFILES; do
      get_profile_path "$profile"
    done
    return
  fi

  find "$ENV_DIR" -maxdepth 1 -type f -name '*.env' | sort
}

load_resolved_profiles() {
  [[ -d "$ENV_DIR" ]] || fatal "benchmark env directory not found: $ENV_DIR"

  RESOLVED_PROFILES=()
  local profile
  while IFS= read -r profile; do
    RESOLVED_PROFILES+=("$profile")
  done < <(resolve_profiles)

  [[ "${#RESOLVED_PROFILES[@]}" -gt 0 ]] || fatal "no benchmark profiles resolved"
}

ensure_benchmark_configs() {
  [[ -d "$ENV_DIR" ]] || fatal "benchmark env directory not found: $ENV_DIR"

  local profile
  for profile in "${RESOLVED_PROFILES[@]}"; do
    [[ -f "$profile" ]] || fatal "profile env file not found: $profile"
  done

  local platform workflow stack
  for platform in $PLATFORMS; do
    case "$platform" in
      tinyfaas | faasd) ;;
      *) fatal "unsupported platform: $platform" ;;
    esac
    for workflow in $WORKFLOWS; do
      stack="$(get_stack_file_path "$platform" "$workflow")"
      [[ -f "$stack" ]] || fatal "workflow stack not found: $stack"
    done
  done
}

ensure_benchmark_envs() {
  [[ -d "$TERRAFORM_DIR" ]] || fatal "terraform directory not found: $TERRAFORM_DIR"
  [[ -f "$SSH_PRIVATE_KEY" ]] || fatal "SSH private key not found: $SSH_PRIVATE_KEY"
  [[ -f "$SSH_PUBLIC_KEY" ]] || fatal "SSH public key not found: $SSH_PUBLIC_KEY"

  # Use GITHUB_TOKEN for Terraform if TF_VAR_github_token is not set
  if [[ -z "${TF_VAR_github_token:-}" && -n "${GITHUB_TOKEN:-}" ]]; then
    export TF_VAR_github_token="$GITHUB_TOKEN"
  fi

  if [[ -z "${TF_VAR_github_token:-}" ]]; then
    fatal "set TF_VAR_github_token or GITHUB_TOKEN before running benchmarks"
  fi
}

terraform_args() {
  local platform="$1"
  local profile="$2"
  printf '%s\0' \
    -var "faas_platforms=[\"$platform\"]" \
    -var "env_file=$profile" \
    -var "machine_type=$MACHINE_TYPE" \
    -var "ssh_pubkey=$SSH_PUBLIC_KEY" \
    -var "ssh_user=$SSH_USER"
}

run_terraform() {
  local log_file="$1"
  local state_file="$2"
  shift
  shift
  log "terraform $*"
  terraform -chdir="$TERRAFORM_DIR" "$@" -state="$state_file" >"$log_file" 2>&1
}

terraform_init() {
  log "terraform init"
  terraform -chdir="$TERRAFORM_DIR" init > /dev/null 2>&1 || fatal "terraform init failed"
}

terraform_destroy() {
  local platform="$1"
  local profile="$2"
  local run_dir="$3"
  local log_file="$4"
  local arg
  local -a args=()
  while IFS= read -r -d '' arg; do
    args+=("$arg")
  done < <(terraform_args "$platform" "$profile")
  run_terraform "$log_file" "$run_dir/terraform.tfstate" destroy -auto-approve "${args[@]}"
}

terraform_apply() {
  local platform="$1"
  local profile="$2"
  local run_dir="$3"
  local log_file="$4"
  local arg
  local -a args=()
  while IFS= read -r -d '' arg; do
    args+=("$arg")
  done < <(terraform_args "$platform" "$profile")
  run_terraform "$log_file" "$run_dir/terraform.tfstate" apply -auto-approve "${args[@]}"
}

terraform_output_json() {
  local run_dir="$1"
  local name="$2"
  terraform -chdir="$TERRAFORM_DIR" output -state="$run_dir/terraform.tfstate" -json "$name"
}

sanitize_terraform_outputs() {
  local platform="$1"
  local run_dir="$2"
  local output_file="$3"
  local public_ip gateway_url instance_name zone deployed_platform
  public_ip="$(terraform_output_json "$run_dir" public_ips | jq -r --arg platform "$platform" '.[$platform]')" || return
  gateway_url="$(terraform_output_json "$run_dir" gateway_urls | jq -r --arg platform "$platform" '.[$platform]')" || return
  instance_name="$(terraform_output_json "$run_dir" instance_names | jq -r --arg platform "$platform" '.[$platform]')" || return
  zone="$(terraform_output_json "$run_dir" zones | jq -r --arg platform "$platform" '.[$platform]')" || return
  deployed_platform="$(terraform_output_json "$run_dir" deployed_faas_platforms | jq -r --arg platform "$platform" '.[$platform]')" || return

  jq -n \
    --arg public_ip "$public_ip" \
    --arg gateway_url "$gateway_url" \
    --arg instance_name "$instance_name" \
    --arg zone "$zone" \
    --arg deployed_faas_platform "$deployed_platform" \
    '{
      public_ip: $public_ip,
      gateway_url: $gateway_url,
      instance_name: $instance_name,
      zone: $zone,
      deployed_faas_platform: $deployed_faas_platform
    }' >"$output_file"
}

ssh_base() {
  local host="$1"
  shift
  ssh \
    -n \
    -i "$SSH_PRIVATE_KEY" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ConnectTimeout=15 \
    "$SSH_USER@$host" \
    "$@"
}

scp_from_vm() {
  local host="$1"
  local remote_path="$2"
  local local_path="$3"
  scp \
    -i "$SSH_PRIVATE_KEY" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    -o ConnectTimeout=15 \
    "$SSH_USER@$host:$remote_path" "$local_path"
}

http_auth_args() {
  local platform="$1"
  local auth_user="${2:-}"
  local auth_password="${3:-}"
  if [[ "$platform" == "faasd" ]]; then
    printf '%s\0' -u "$auth_user:$auth_password"
  fi
}

curl_json() {
  local platform="$1"
  local auth_user="$2"
  local auth_password="$3"
  local url="$4"
  local output="$5"
  local arg
  local -a auth_args=()

  while IFS= read -r -d '' arg; do
    auth_args+=("$arg")
  done < <(http_auth_args "$platform" "$auth_user" "$auth_password")

  curl --fail --silent --show-error "${auth_args[@]}" "$url" -o "$output"
}

get_list_endpoint() {
  case "$1" in
    tinyfaas) printf '/system/list\n' ;;
    faasd) printf '/system/functions\n' ;;
    *) fatal "unsupported platform: $1" ;;
  esac
}

get_stats_endpoint() {
  local platform="$1"
  local gateway_url="$2"
  local function_name="$3"
  case "$platform" in
    tinyfaas) printf '%s/system/stats/function/%s\n' "$gateway_url" "$function_name" ;;
    faasd) printf '%s/system/stats/function/%s?namespace=openfaas-fn\n' "$gateway_url" "$function_name" ;;
    *) fatal "unsupported platform: $platform" ;;
  esac
}

collect_function_names() {
  local platform="$1"
  local gateway_url="$2"
  local auth_user="$3"
  local auth_password="$4"
  local output_file="$5"
  local list_path tmp_file
  list_path="$(get_list_endpoint "$platform")"
  tmp_file="$(mktemp)"

  curl_json "$platform" "$auth_user" "$auth_password" "$gateway_url$list_path" "$tmp_file" || {
    rm -f "$tmp_file"
    return 1
  }
  jq -r '.[].name // empty' "$tmp_file" | sort >"$output_file" || {
    rm -f "$tmp_file"
    return 1
  }
  rm -f "$tmp_file"
}

utc_now() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}

update_metadata_json() {
  local file="$1"
  shift
  local tmp_file
  tmp_file="$(mktemp)"
  jq "$@" "$file" >"$tmp_file" && mv "$tmp_file" "$file"
}

init_metadata() {
  local file="$1"
  local profile_name="$2"
  local profile_path="$3"
  local platform="$4"
  local workflow="$5"
  local run_dir="$6"
  local stack_path="$7"

  jq -n \
    --arg profile "$profile_name" \
    --arg profile_path "$profile_path" \
    --arg platform "$platform" \
    --arg workflow "$workflow" \
    --arg run_dir "$run_dir" \
    --arg machine_type "$MACHINE_TYPE" \
    --arg ssh_user "$SSH_USER" \
    --arg stack_path "$stack_path" \
    --argjson k6_iterations "$K6_ITERATIONS" \
    --argjson k6_vus "$K6_VUS" \
    '{
      profile: $profile,
      profile_path: $profile_path,
      platform: $platform,
      workflow: $workflow,
      run_dir: $run_dir,
      gateway_url: null,
      public_ip: null,
      instance_name: null,
      zone: null,
      machine_type: $machine_type,
      ssh_user: $ssh_user,
      stack_path: $stack_path,
      k6: {
        iterations: $k6_iterations,
        vus: $k6_vus
      },
      provision_started_at: null,
      provision_finished_at: null,
      k6_run_started_at: null,
      k6_run_finished_at: null,
      status: "running",
      message: ""
    }' >"$file"
}

set_metadata_time() {
  local file="$1"
  local field="$2"
  [[ -f "$file" ]] || return 0
  update_metadata_json "$file" \
    --arg field "$field" \
    --arg timestamp "$(utc_now)" \
    '.[$field] = $timestamp'
}

set_metadata_terraform_outputs() {
  local file="$1"
  local gateway_url="$2"
  local public_ip="$3"
  local instance_name="$4"
  local zone="$5"
  update_metadata_json "$file" \
    --arg gateway_url "$gateway_url" \
    --arg public_ip "$public_ip" \
    --arg instance_name "$instance_name" \
    --arg zone "$zone" \
    '.gateway_url = $gateway_url
      | .public_ip = $public_ip
      | .instance_name = $instance_name
      | .zone = $zone'
}

set_metadata_status() {
  local file="$1"
  local status="$2"
  local message="${3:-}"
  if [[ ! -f "$file" ]]; then
    jq -n \
      --arg status "$status" \
      --arg message "$message" \
      '{
        gateway_url: null,
        public_ip: null,
        instance_name: null,
        zone: null,
        provision_started_at: null,
        provision_finished_at: null,
        k6_run_started_at: null,
        k6_run_finished_at: null,
        status: $status,
        message: $message
      }' >"$file"
    return
  fi

  update_metadata_json "$file" \
    --arg status "$status" \
    --arg message "$message" \
    '.status = $status | .message = $message'
}

deploy_workflow() {
  local platform="$1"
  local stack_path="$2"
  local gateway_url="$3"
  local auth_user="$4"
  local auth_password="$5"
  local log_file="$6"

  if [[ "$platform" == "faasd" ]]; then
    log "logging into faasd gateway"
    printf '%s' "$auth_password" | faas-cli login \
      --username "$auth_user" \
      --password-stdin \
      --gateway "$gateway_url" >>"$log_file" 2>&1 || return
  fi

  log "deploying $platform workflow from $stack_path"
  log "workflow resource limits: CPU=$WORKFLOW_CPU_LIMIT, Memory=$WORKFLOW_MEMORY_LIMIT"
  WORKFLOW_CPU_LIMIT=$WORKFLOW_CPU_LIMIT \
    WORKFLOW_MEMORY_LIMIT=$WORKFLOW_MEMORY_LIMIT \
    faas-cli deploy \
      --gateway "$gateway_url" \
      -f "$stack_path" >>"$log_file" 2>&1 || return
}

run_k6() {
  local platform="$1"
  local workflow="$2"
  local gateway_url="$3"
  local auth_user="$4"
  local auth_password="$5"
  local run_id="$6"
  local run_dir="$7"
  local script
  script="$(get_workflow_k6_script "$workflow")"

  local -a env_args=(
    -e "PLATFORM=$platform"
    -e "WORKFLOW=$(get_k6_workflow_name "$workflow")"
    -e "GATEWAY_URL=$gateway_url"
    -e "ITERATIONS=$K6_ITERATIONS"
    -e "VUS=$K6_VUS"
    -e "MAX_DURATION=$K6_MAX_DURATION"
    -e "GRACEFUL_STOP=$K6_GRACEFUL_STOP"
    -e "RUN_ID=$run_id"
    -e "RUN_LABEL=$run_id"
  )

  if [[ "$platform" == "faasd" ]]; then
    env_args+=(
      -e "BASIC_AUTH_USER=$auth_user"
      -e "BASIC_AUTH_PASSWORD=$auth_password"
    )
  fi

  log "running k6 for $platform/$workflow"
  k6 run \
    "${env_args[@]}" \
    --quiet \
    --summary-export "$run_dir/k6/summary.json" \
    --out "csv=$run_dir/k6/metrics.csv" \
    "$script" >"$run_dir/logs/k6-run.log" 2>&1
}

collect_stats() {
  local platform="$1"
  local gateway_url="$2"
  local auth_user="$3"
  local auth_password="$4"
  local run_dir="$5"
  local function_names_file="$run_dir/stats/function-names.txt"

  log "collecting function names from $platform list endpoint"
  collect_function_names "$platform" "$gateway_url" "$auth_user" "$auth_password" "$function_names_file" || return

  log "collecting callgraph data"
  curl_json "$platform" "$auth_user" "$auth_password" "$gateway_url/system/callgraph" "$run_dir/stats/callgraph.json" || true

  local function_name url output
  while IFS= read -r function_name; do
    [[ -n "$function_name" ]] || continue
    url="$(get_stats_endpoint "$platform" "$gateway_url" "$function_name")"
    output="$run_dir/stats/functions/$function_name.json"
    curl_json "$platform" "$auth_user" "$auth_password" "$url" "$output" || true
  done <"$function_names_file"
}

collect_remote_logs() {
  local platform="$1"
  local public_ip="$2"
  local run_dir="$3"
  local services=()

  case "$platform" in
    tinyfaas) services=(tf-gateway tf-manager tf-rproxy) ;;
    faasd) services=(faasd faasd-provider faasd-gateway containerd) ;;
    *) fatal "unsupported platform: $platform" ;;
  esac

  log "collecting VM provision log"
  scp_from_vm "$public_ip" "/var/log/vm-provision.log" "$run_dir/logs/vm-provision.log" >/dev/null 2>&1 || true

  local service
  for service in "${services[@]}"; do
    log "collecting systemd log for $service"
    ssh_base "$public_ip" "sudo journalctl -u '$service' -o cat --no-pager" \
      >"$run_dir/logs/systemd-$service.log" 2>&1 || true
  done
}

create_run_dirs() {
  local run_dir="$1"
  mkdir -p \
    "$run_dir/k6" \
    "$run_dir/stats/functions" \
    "$run_dir/logs"
}

get_run_dir() {
  local profile_name="$1"
  local platform="$2"
  local workflow_name="$3"
  local base_dir="$OUTPUT_ROOT/$profile_name/$platform/$workflow_name"

  if bool_true "$RERUN_OVERWRITE"; then
    printf '%s\n' "$base_dir"
    return
  fi

  if [[ -e "$base_dir" ]]; then
    printf '%s_%s\n' "$base_dir" "$RUN_TIMESTAMP"
  else
    printf '%s\n' "$base_dir"
  fi
}

on_exit() {
  local status=$?
  if [[ "$EXIT_STATUS" -ne 0 ]]; then
    status="$EXIT_STATUS"
  fi
  exit "$status"
}

handle_interrupt() {
  local signal="$1"
  INTERRUPTED="true"
  trap - INT TERM

  case "$signal" in
    INT) EXIT_STATUS=130 ;;
    TERM) EXIT_STATUS=143 ;;
    *) EXIT_STATUS=130 ;;
  esac

  log "received SIG$signal; stopping benchmark jobs"
  local pid
  for pid in "${RUN_PIDS[@]:-}"; do
    kill -TERM "$pid" >/dev/null 2>&1 || true
  done
  for pid in "${RUN_PIDS[@]:-}"; do
    wait "$pid" >/dev/null 2>&1 || true
  done
  exit "$EXIT_STATUS"
}

handle_child_interrupt() {
  local signal="$1"
  local status
  trap '' INT TERM

  case "$signal" in
    INT) status=130 ;;
    TERM) status=143 ;;
    *) status=130 ;;
  esac

  log "received SIG$signal in ${CHILD_PLATFORM:-benchmark} worker; cleaning up"
  if [[ -n "$CHILD_RUN_DIR" ]]; then
    set_metadata_status "$CHILD_RUN_DIR/metadata.json" "interrupted" "received SIG$signal" || true
  fi

  if [[ "$CHILD_INFRA_ACTIVE" == "true" && "$CHILD_CLEANUP_DONE" != "true" && -n "$CHILD_PLATFORM" && -n "$CHILD_PROFILE_PATH" && -n "$CHILD_RUN_DIR" ]]; then
    CHILD_CLEANUP_DONE="true"
    log "destroying interrupted infrastructure for $CHILD_PLATFORM; log: $CHILD_RUN_DIR/logs/terraform-destroy-after.log"
    terraform_destroy "$CHILD_PLATFORM" "$CHILD_PROFILE_PATH" "$CHILD_RUN_DIR" "$CHILD_RUN_DIR/logs/terraform-destroy-after.log" || true
  fi

  exit "$status"
}

mark_failed_and_cleanup() {
  local platform="$1"
  local profile_path="$2"
  local run_dir="$3"
  local message="$4"
  local public_ip="${5:-}"

  set_metadata_status "$run_dir/metadata.json" "failed" "$message" || true
  if ! bool_true "$KEEP_INFRA_ON_FAILURE"; then
    if [[ -n "$public_ip" ]]; then
      collect_remote_logs "$platform" "$public_ip" "$run_dir" || true
    fi
    terraform_destroy "$platform" "$profile_path" "$run_dir" "$run_dir/logs/terraform-destroy-after.log" || true
  fi
}

run_benchmark() {
  local profile_path="$1"
  local platform="$2"
  local workflow="$3"
  local profile_name workflow_name run_id run_dir stack_path
  profile_name="$(get_profile_name "$profile_path")"
  workflow_name="$(get_k6_workflow_name "$workflow")"
  run_id="$profile_name-$platform-$workflow_name"
  run_dir="$(get_run_dir "$profile_name" "$platform" "$workflow_name")"
  stack_path="$(get_stack_file_path "$platform" "$workflow")"
  CHILD_PLATFORM="$platform"
  CHILD_PROFILE_PATH="$profile_path"
  CHILD_RUN_DIR="$run_dir"
  CHILD_INFRA_ACTIVE="false"
  CHILD_CLEANUP_DONE="false"

  if bool_true "$RERUN_OVERWRITE"; then
    log "overwriting selected run directory: $run_dir"
    rm -rf -- "$run_dir"
  elif [[ -e "$run_dir" ]]; then
    fatal "resolved run directory already exists: $run_dir"
  fi

  create_run_dirs "$run_dir"
  exec > >(tee -a "$run_dir/logs/benchmark-run.log") 2>&1
  cp "$profile_path" "$run_dir/profile.env"

  log "starting run: $run_id"
  init_metadata \
    "$run_dir/metadata.json" \
    "$profile_name" \
    "$profile_path" \
    "$platform" \
    "$workflow_name" \
    "$run_dir" \
    "$stack_path" || {
      mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "metadata initialization failed"
      CHILD_CLEANUP_DONE="true"
      return 1
    }

  terraform_destroy "$platform" "$profile_path" "$run_dir" "$run_dir/logs/terraform-destroy-before.log" || true
  CHILD_INFRA_ACTIVE="true"
  set_metadata_time "$run_dir/metadata.json" "provision_started_at" || {
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "provision start timestamp write failed"
    CHILD_CLEANUP_DONE="true"
    return 1
  }
  if ! terraform_apply "$platform" "$profile_path" "$run_dir" "$run_dir/logs/terraform-apply.log"; then
    set_metadata_time "$run_dir/metadata.json" "provision_finished_at" || true
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "terraform apply failed"
    CHILD_CLEANUP_DONE="true"
    return 1
  fi
  set_metadata_time "$run_dir/metadata.json" "provision_finished_at" || {
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "provision finish timestamp write failed"
    CHILD_CLEANUP_DONE="true"
    return 1
  }

  sanitize_terraform_outputs "$platform" "$run_dir" "$run_dir/terraform-outputs.json" || {
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "terraform output capture failed"
    CHILD_CLEANUP_DONE="true"
    return 1
  }

  local gateway_url public_ip instance_name zone auth_user auth_password
  gateway_url="$(jq -r '.gateway_url' "$run_dir/terraform-outputs.json")" || return
  public_ip="$(jq -r '.public_ip' "$run_dir/terraform-outputs.json")" || return
  instance_name="$(jq -r '.instance_name' "$run_dir/terraform-outputs.json")" || return
  zone="$(jq -r '.zone' "$run_dir/terraform-outputs.json")" || return
  auth_user=""
  auth_password=""

  set_metadata_terraform_outputs "$run_dir/metadata.json" "$gateway_url" "$public_ip" "$instance_name" "$zone" || {
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "metadata terraform output write failed" "$public_ip"
    CHILD_CLEANUP_DONE="true"
    return 1
  }

  if [[ "$platform" == "faasd" ]]; then
    auth_user="$(terraform_output_json "$run_dir" faasd_auth_users | jq -r --arg platform "$platform" '.[$platform]')" || {
      mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "faasd auth user output failed" "$public_ip"
      CHILD_CLEANUP_DONE="true"
      return 1
    }
    auth_password="$(terraform_output_json "$run_dir" faasd_auth_passwords | jq -r --arg platform "$platform" '.[$platform]')" || {
      mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "faasd auth password output failed" "$public_ip"
      CHILD_CLEANUP_DONE="true"
      return 1
    }
  fi

  if ! deploy_workflow "$platform" "$stack_path" "$gateway_url" "$auth_user" "$auth_password" "$run_dir/logs/workflow-deploy.log"; then
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "workflow deploy failed" "$public_ip"
    CHILD_CLEANUP_DONE="true"
    return 1
  fi

  set_metadata_time "$run_dir/metadata.json" "k6_run_started_at" || {
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "k6 start timestamp write failed" "$public_ip"
    CHILD_CLEANUP_DONE="true"
    return 1
  }
  if ! run_k6 "$platform" "$workflow_name" "$gateway_url" "$auth_user" "$auth_password" "$run_id" "$run_dir"; then
    set_metadata_time "$run_dir/metadata.json" "k6_run_finished_at" || true
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "k6 run failed" "$public_ip"
    CHILD_CLEANUP_DONE="true"
    return 1
  fi
  set_metadata_time "$run_dir/metadata.json" "k6_run_finished_at" || {
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "k6 finish timestamp write failed" "$public_ip"
    CHILD_CLEANUP_DONE="true"
    return 1
  }

  if ! collect_stats "$platform" "$gateway_url" "$auth_user" "$auth_password" "$run_dir"; then
    mark_failed_and_cleanup "$platform" "$profile_path" "$run_dir" "stats collection failed" "$public_ip"
    CHILD_CLEANUP_DONE="true"
    return 1
  fi

  collect_remote_logs "$platform" "$public_ip" "$run_dir" || true
  set_metadata_status "$run_dir/metadata.json" "success" || {
    if ! bool_true "$KEEP_INFRA_ON_FAILURE"; then
      terraform_destroy "$platform" "$profile_path" "$run_dir" "$run_dir/logs/terraform-destroy-after.log" || true
    fi
    CHILD_CLEANUP_DONE="true"
    return 1
  }

  terraform_destroy "$platform" "$profile_path" "$run_dir" "$run_dir/logs/terraform-destroy-after.log" || true
  CHILD_INFRA_ACTIVE="false"
  CHILD_CLEANUP_DONE="true"
  log "completed run: $run_id"
}

run_platform_profile() {
  local profile="$1"
  local platform="$2"
  local workflow run_failed

  for workflow in $WORKFLOWS; do
    run_failed="false"
    log "starting platform benchmark step: $(get_profile_name "$profile")/$platform/$(get_k6_workflow_name "$workflow")"
    if ! run_benchmark "$profile" "$platform" "$workflow"; then
      run_failed="true"
      log "platform benchmark step failed: $(get_profile_name "$profile")/$platform/$(get_k6_workflow_name "$workflow")"
    fi

    if [[ "$run_failed" == "true" ]] && ! bool_true "$CONTINUE_ON_ERROR"; then
      return 1
    fi
  done
}

run_profiles_parallel() {
  local profile="$1"
  local platform pid failed index status
  failed="false"
  RUN_PIDS=()
  RUN_PID_PLATFORMS=()

  for platform in $PLATFORMS; do
    log "launching platform benchmark $(get_profile_name "$profile")/$platform in background"
    (
      trap 'handle_child_interrupt INT' INT
      trap 'handle_child_interrupt TERM' TERM
      run_platform_profile "$profile" "$platform"
    ) &
    pid="$!"
    RUN_PIDS+=("$pid")
    RUN_PID_PLATFORMS+=("$platform")
    log "launched $platform platform benchmark pid=$pid"
  done

  for index in "${!RUN_PIDS[@]}"; do
    pid="${RUN_PIDS[$index]}"
    platform="${RUN_PID_PLATFORMS[$index]}"
    log "waiting for $platform platform benchmark pid=$pid"
    status=0
    wait "$pid" || status="$?"
    if [[ "$status" -ne 0 ]]; then
      failed="true"
      log "$platform platform benchmark pid=$pid failed with exit status $status"
    else
      log "$platform platform benchmark pid=$pid finished successfully"
    fi
  done
  RUN_PIDS=()
  RUN_PID_PLATFORMS=()

  [[ "$failed" != "true" ]]
}

write_manifest() {
  mkdir -p "$OUTPUT_ROOT"
  local manifest_file="$OUTPUT_ROOT/manifest.json"
  local profile profile_names=""
  for profile in "${RESOLVED_PROFILES[@]}"; do
    profile_names+="$(get_profile_name "$profile") "
  done

  [[ -f "$manifest_file" ]] || printf '[]\n' >"$manifest_file"

  local new_manifest
  new_manifest="$(mktemp)"
  jq -n \
    --arg created_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg output_root "$OUTPUT_ROOT" \
    --arg machine_type "$MACHINE_TYPE" \
    --arg ssh_public_key "$SSH_PUBLIC_KEY" \
    --arg ssh_user "$SSH_USER" \
    --arg profiles "$profile_names" \
    --arg platforms "$PLATFORMS" \
    --arg workflows "$WORKFLOWS" \
    --argjson k6_iterations "$K6_ITERATIONS" \
    --argjson k6_vus "$K6_VUS" \
    '{
      created_at: $created_at,
      output_root: $output_root,
      machine_type: $machine_type,
      ssh_public_key: $ssh_public_key,
      ssh_user: $ssh_user,
      profiles: ($profiles | split(" ") | map(select(length > 0))),
      platforms: ($platforms | split(" ") | map(select(length > 0))),
      workflows: ($workflows | split(" ") | map(select(length > 0))),
      k6: {
        iterations: $k6_iterations,
        vus: $k6_vus
      }
    }' >"$new_manifest"

  jq --slurpfile new_manifest "$new_manifest" '. + $new_manifest' "$manifest_file" >"$manifest_file.tmp"
  mv "$manifest_file.tmp" "$manifest_file"
  rm -f "$new_manifest"
}

print_benchmark_configs() {
  echo -e "OUTPUT_ROOT: $OUTPUT_ROOT"
  echo -e "MACHINE_TYPE: $MACHINE_TYPE"
  echo -e "SSH_PUBLIC_KEY: $SSH_PUBLIC_KEY"
  echo -e "SSH_USER: $SSH_USER"
  echo -e "PLATFORMS: $PLATFORMS"
  echo -e "WORKFLOWS: $WORKFLOWS"
  echo -e "WORKFLOW_CPU_LIMIT: $WORKFLOW_CPU_LIMIT"
  echo -e "WORKFLOW_MEMORY_LIMIT: $WORKFLOW_MEMORY_LIMIT"
  echo -e "K6_ITERATIONS: $K6_ITERATIONS"
  echo -e "K6_VUS: $K6_VUS"
  echo -e "CONTINUE_ON_ERROR: $CONTINUE_ON_ERROR"
  echo -e "KEEP_INFRA_ON_FAILURE: $KEEP_INFRA_ON_FAILURE"
  echo -e "RERUN_OVERWRITE: $RERUN_OVERWRITE"
  echo -e "DRY_RUN: $DRY_RUN"
  echo -e "\nPROFILE\tPLATFORM\tWORKFLOW\tRUN_DIR\tSTACK_PATH"
  local profile platform workflow profile_name workflow_name stack run_dir
  for profile in "${RESOLVED_PROFILES[@]}"; do
    profile_name="$(get_profile_name "$profile")"
    for platform in $PLATFORMS; do
      for workflow in $WORKFLOWS; do
        workflow_name="$(get_k6_workflow_name "$workflow")"
        stack="$(get_stack_file_path "$platform" "$workflow")"
        run_dir="$(get_run_dir "$profile_name" "$platform" "$workflow_name")"
        printf '%s\t%s\t%s\t%s\t%s\n' "$profile_name" "$platform" "$workflow_name" "$run_dir" "$stack"
      done
    done
  done
}

main() {
  cd "$ROOT_DIR"

  ensure_dependencies
  load_resolved_profiles
  ensure_benchmark_configs

  if bool_true "$DRY_RUN"; then
    print_benchmark_configs
    return
  fi

  ensure_benchmark_envs

  trap 'handle_interrupt INT' INT
  trap 'handle_interrupt TERM' TERM
  trap on_exit EXIT

  log "initializing Terraform"
  terraform_init
  write_manifest

  local profile run_failed
  for profile in "${RESOLVED_PROFILES[@]}"; do
    run_failed="false"
    if ! run_profiles_parallel "$profile"; then
      run_failed="true"
      log "platform benchmark failed for profile: $(get_profile_name "$profile")"
    fi

    if [[ "$INTERRUPTED" == "true" ]]; then
      EXIT_STATUS=130
      fatal "stopping after interrupt"
    fi

    if [[ "$run_failed" == "true" ]] && ! bool_true "$CONTINUE_ON_ERROR"; then
      fatal "stopping after failed run"
    fi
  done
}

main "$@"
