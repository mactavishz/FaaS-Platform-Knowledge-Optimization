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
PLATFORMS="${PLATFORMS:-tinyfaas faasd}"
WORKFLOWS="${WORKFLOWS:-iot tree webshop}"
OUTPUT_ROOT="${OUTPUT_ROOT:-$BENCHMARK_DIR/results/$RUN_NAME}"
MACHINE_TYPE="${MACHINE_TYPE:-n2-highcpu-32}"
SSH_PRIVATE_KEY="${SSH_PRIVATE_KEY:-$TERRAFORM_DIR/gcp}"
SSH_PUBLIC_KEY="${SSH_PUBLIC_KEY:-$TERRAFORM_DIR/gcp.pub}"
SSH_USER="${SSH_USER:-bench}"
K6_ITERATIONS="${K6_ITERATIONS:-70}"
K6_VUS="${K6_VUS:-1}"
K6_MAX_DURATION="${K6_MAX_DURATION:-6h}"
K6_GRACEFUL_STOP="${K6_GRACEFUL_STOP:-30s}"
CONTINUE_ON_ERROR="${CONTINUE_ON_ERROR:-false}"
KEEP_INFRA_ON_FAILURE="${KEEP_INFRA_ON_FAILURE:-false}"
DRY_RUN="${DRY_RUN:-false}"

CURRENT_RUN_DIR=""
CURRENT_PLATFORM=""
CURRENT_INFRA_ACTIVE="false"
CURRENT_RUN_FAILED="false"
CURRENT_PROFILE_PATH=""
INTERRUPTED="false"
CLEANUP_IN_PROGRESS="false"
EXIT_STATUS=0

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2
}

fatal() {
  log "ERROR: $*"
  exit 1
}

bool_true() {
  case "${1,,}" in
    1 | true | yes | y) return 0 ;;
    *) return 1 ;;
  esac
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

profile_path_for() {
  local profile="$1"
  if [[ "$profile" = */* || "$profile" = *.env ]]; then
    abs_path "$profile"
  else
    printf '%s\n' "$ENV_DIR/$profile.env"
  fi
}

profile_name_for() {
  local path="$1"
  basename "$path" .env
}

get_workflow_dir_name() {
  case "${1,,}" in
    iot) printf 'IoT\n' ;;
    tree) printf 'tree\n' ;;
    webshop) printf 'webshop\n' ;;
    linear3) fatal "linear3 is intentionally excluded from benchmark runs" ;;
    *) fatal "unsupported workflow: $1" ;;
  esac
}

get_k6_workflow_name() {
  case "${1,,}" in
    iot) printf 'iot\n' ;;
    tree) printf 'tree\n' ;;
    webshop) printf 'webshop\n' ;;
    *) fatal "unsupported workflow: $1" ;;
  esac
}

get_workflow_k6_script() {
  case "${1,,}" in
    iot | tree) printf '%s\n' "$BENCHMARK_DIR/scripts/workflow_cold_latency.js" ;;
    webshop) printf '%s\n' "$BENCHMARK_DIR/scripts/webshop_user_journey.js" ;;
    *) fatal "unsupported workflow: $1" ;;
  esac
}

stack_path_for() {
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
      profile_path_for "$profile"
    done
    return
  fi

  find "$ENV_DIR" -maxdepth 1 -type f -name '*.env' | sort
}

ensure_benchmark_configs() {
  [[ -d "$ENV_DIR" ]] || fatal "benchmark env directory not found: $ENV_DIR"

  local profile
  while IFS= read -r profile; do
    [[ -f "$profile" ]] || fatal "profile env file not found: $profile"
  done < <(resolve_profiles)

  local platform workflow stack
  for platform in $PLATFORMS; do
    case "$platform" in
      tinyfaas | faasd) ;;
      *) fatal "unsupported platform: $platform" ;;
    esac
    for workflow in $WORKFLOWS; do
      stack="$(stack_path_for "$platform" "$workflow")"
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
    -var "faas_platform=$platform" \
    -var "env_file=$profile" \
    -var "machine_type=$MACHINE_TYPE" \
    -var "ssh_pubkey=$SSH_PUBLIC_KEY" \
    -var "ssh_user=$SSH_USER"
}

run_terraform() {
  local log_file="$1"
  shift
  log "terraform $*"
  terraform -chdir="$TERRAFORM_DIR" "$@" >"$log_file" 2>&1
}

terraform_init() {
  log "terraform init"
  terraform -chdir="$TERRAFORM_DIR" init > /dev/null 2>&1 || fatal "terraform init failed"
}

terraform_destroy() {
  local platform="$1"
  local profile="$2"
  local log_file="$3"
  local -a args
  readarray -d '' -t args < <(terraform_args "$platform" "$profile")
  run_terraform "$log_file" destroy -auto-approve "${args[@]}"
}

terraform_apply() {
  local platform="$1"
  local profile="$2"
  local log_file="$3"
  local -a args
  readarray -d '' -t args < <(terraform_args "$platform" "$profile")
  run_terraform "$log_file" apply -auto-approve "${args[@]}"
}

terraform_output_raw() {
  terraform -chdir="$TERRAFORM_DIR" output -raw "$1"
}

capture_sanitized_outputs() {
  local output_file="$1"
  local public_ip gateway_url instance_name zone deployed_platform
  public_ip="$(terraform_output_raw public_ip)" || return
  gateway_url="$(terraform_output_raw gateway_url)" || return
  instance_name="$(terraform_output_raw instance_name)" || return
  zone="$(terraform_output_raw zone)" || return
  deployed_platform="$(terraform_output_raw deployed_faas_platform)" || return

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
  local -a auth_args=()
  readarray -d '' -t auth_args < <(http_auth_args "$platform" "$auth_user" "$auth_password")

  curl --fail --silent --show-error "${auth_args[@]}" "$url" -o "$output"
}

list_endpoint_for() {
  case "$1" in
    tinyfaas) printf '/system/list\n' ;;
    faasd) printf '/system/functions\n' ;;
    *) fatal "unsupported platform: $1" ;;
  esac
}

stats_url_for() {
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
  list_path="$(list_endpoint_for "$platform")"
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

write_metadata() {
  local file="$1"
  local profile_name="$2"
  local profile_path="$3"
  local platform="$4"
  local workflow="$5"
  local run_dir="$6"
  local gateway_url="$7"
  local public_ip="$8"
  local instance_name="$9"
  local zone="${10}"
  local stack_path="${11}"

  jq -n \
    --arg started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg profile "$profile_name" \
    --arg profile_path "$profile_path" \
    --arg platform "$platform" \
    --arg workflow "$workflow" \
    --arg run_dir "$run_dir" \
    --arg gateway_url "$gateway_url" \
    --arg public_ip "$public_ip" \
    --arg instance_name "$instance_name" \
    --arg zone "$zone" \
    --arg machine_type "$MACHINE_TYPE" \
    --arg ssh_user "$SSH_USER" \
    --arg stack_path "$stack_path" \
    --argjson k6_iterations "$K6_ITERATIONS" \
    --argjson k6_vus "$K6_VUS" \
    '{
      started_at: $started_at,
      profile: $profile,
      profile_path: $profile_path,
      platform: $platform,
      workflow: $workflow,
      run_dir: $run_dir,
      gateway_url: $gateway_url,
      public_ip: $public_ip,
      instance_name: $instance_name,
      zone: $zone,
      machine_type: $machine_type,
      ssh_user: $ssh_user,
      stack_path: $stack_path,
      k6: {
        iterations: $k6_iterations,
        vus: $k6_vus
      }
    }' >"$file"
}

write_status() {
  local file="$1"
  local status="$2"
  local message="${3:-}"
  jq -n \
    --arg finished_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg status "$status" \
    --arg message "$message" \
    '{finished_at: $finished_at, status: $status, message: $message}' >"$file"
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
  WORKFLOW_CPU_LIMIT=1 \
    WORKFLOW_MEMORY_LIMIT=1024Mi \
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
    --summary-mode full \
    --summary-export "$run_dir/k6/summary.json" \
    --out "json=$run_dir/k6/metrics.json" \
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
    url="$(stats_url_for "$platform" "$gateway_url" "$function_name")"
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

cleanup_current_infra() {
  if [[ "$CLEANUP_IN_PROGRESS" == "true" ]]; then
    log "cleanup already in progress; skipping nested cleanup request"
    return
  fi
  if [[ "$CURRENT_INFRA_ACTIVE" != "true" || -z "$CURRENT_RUN_DIR" || -z "$CURRENT_PLATFORM" ]]; then
    return
  fi
  if [[ "$CURRENT_RUN_FAILED" == "true" && "$INTERRUPTED" != "true" ]] && bool_true "$KEEP_INFRA_ON_FAILURE"; then
    log "keeping infrastructure for failed run: $CURRENT_RUN_DIR"
    return
  fi

  CLEANUP_IN_PROGRESS="true"
  log "destroying infrastructure for current run"
  if [[ -n "${CURRENT_PROFILE_PATH:-}" ]]; then
    terraform_destroy "$CURRENT_PLATFORM" "$CURRENT_PROFILE_PATH" "$CURRENT_RUN_DIR/logs/terraform-destroy-after.log" || true
  fi
  CURRENT_INFRA_ACTIVE="false"
  CLEANUP_IN_PROGRESS="false"
}

on_exit() {
  local status=$?
  if [[ "$EXIT_STATUS" -ne 0 ]]; then
    status="$EXIT_STATUS"
  fi
  cleanup_current_infra
  exit "$status"
}

handle_interrupt() {
  local signal="$1"
  INTERRUPTED="true"
  CURRENT_RUN_FAILED="true"

  case "$signal" in
    INT) EXIT_STATUS=130 ;;
    TERM) EXIT_STATUS=143 ;;
    *) EXIT_STATUS=130 ;;
  esac

  log "received SIG$signal; stopping benchmark and cleaning up infrastructure"
  if [[ -n "$CURRENT_RUN_DIR" ]]; then
    write_status "$CURRENT_RUN_DIR/status.json" "interrupted" "received SIG$signal" || true
  fi
  cleanup_current_infra
  exit "$EXIT_STATUS"
}

run_one() {
  local profile_path="$1"
  local platform="$2"
  local workflow="$3"
  local profile_name workflow_name run_id run_dir stack_path
  profile_name="$(profile_name_for "$profile_path")"
  workflow_name="$(get_k6_workflow_name "$workflow")"
  run_id="$profile_name-$platform-$workflow_name"
  run_dir="$OUTPUT_ROOT/$profile_name/$platform/$workflow_name"
  stack_path="$(stack_path_for "$platform" "$workflow")"

  CURRENT_RUN_DIR="$run_dir"
  CURRENT_PLATFORM="$platform"
  CURRENT_PROFILE_PATH="$profile_path"
  CURRENT_INFRA_ACTIVE="false"
  CURRENT_RUN_FAILED="false"

  create_run_dirs "$run_dir"
  cp "$profile_path" "$run_dir/profile.env"

  log "starting run: $run_id"
  terraform_destroy "$platform" "$profile_path" "$run_dir/logs/terraform-destroy-before.log" || true
  CURRENT_INFRA_ACTIVE="true"
  terraform_apply "$platform" "$profile_path" "$run_dir/logs/terraform-apply.log" || return

  capture_sanitized_outputs "$run_dir/terraform-outputs.json" || return

  local gateway_url public_ip instance_name zone auth_user auth_password
  gateway_url="$(jq -r '.gateway_url' "$run_dir/terraform-outputs.json")" || return
  public_ip="$(jq -r '.public_ip' "$run_dir/terraform-outputs.json")" || return
  instance_name="$(jq -r '.instance_name' "$run_dir/terraform-outputs.json")" || return
  zone="$(jq -r '.zone' "$run_dir/terraform-outputs.json")" || return
  auth_user=""
  auth_password=""

  if [[ "$platform" == "faasd" ]]; then
    auth_user="$(terraform_output_raw faasd_auth_user)" || return
    auth_password="$(terraform_output_raw faasd_auth_password)" || return
  fi

  write_metadata \
    "$run_dir/metadata.json" \
    "$profile_name" \
    "$profile_path" \
    "$platform" \
    "$workflow_name" \
    "$run_dir" \
    "$gateway_url" \
    "$public_ip" \
    "$instance_name" \
    "$zone" \
    "$stack_path" || return

  deploy_workflow "$platform" "$stack_path" "$gateway_url" "$auth_user" "$auth_password" "$run_dir/logs/workflow-deploy.log" || return
  run_k6 "$platform" "$workflow_name" "$gateway_url" "$auth_user" "$auth_password" "$run_id" "$run_dir" || return
  collect_stats "$platform" "$gateway_url" "$auth_user" "$auth_password" "$run_dir" || return
  collect_remote_logs "$platform" "$public_ip" "$run_dir" || true
  write_status "$run_dir/status.json" "success" || return

  cleanup_current_infra
  log "completed run: $run_id"
}

write_manifest() {
  mkdir -p "$OUTPUT_ROOT"
  jq -n \
    --arg created_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg output_root "$OUTPUT_ROOT" \
    --arg machine_type "$MACHINE_TYPE" \
    --arg ssh_public_key "$SSH_PUBLIC_KEY" \
    --arg ssh_user "$SSH_USER" \
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
      platforms: ($platforms | split(" ") | map(select(length > 0))),
      workflows: ($workflows | split(" ") | map(select(length > 0))),
      k6: {
        iterations: $k6_iterations,
        vus: $k6_vus
      }
    }' >"$OUTPUT_ROOT/manifest.json"
}

print_benchmark_configs() {
  echo -e "OUTPUT_ROOT: $OUTPUT_ROOT"
  echo -e "MACHINE_TYPE: $MACHINE_TYPE"
  echo -e "SSH_PUBLIC_KEY: $SSH_PUBLIC_KEY"
  echo -e "SSH_USER: $SSH_USER"
  echo -e "PLATFORMS: $PLATFORMS"
  echo -e "WORKFLOWS: $WORKFLOWS"
  echo -e "K6_ITERATIONS: $K6_ITERATIONS"
  echo -e "K6_VUS: $K6_VUS"
  echo -e "CONTINUE_ON_ERROR: $CONTINUE_ON_ERROR"
  echo -e "KEEP_INFRA_ON_FAILURE: $KEEP_INFRA_ON_FAILURE"
  echo -e "DRY_RUN: $DRY_RUN"
  echo -e "\nPROFILE\tPLATFORM\tWORKFLOW\tSTACK_PATH"
  local profile platform workflow profile_name stack
  while IFS= read -r profile; do
    profile_name="$(profile_name_for "$profile")"
    for platform in $PLATFORMS; do
      for workflow in $WORKFLOWS; do
        stack="$(stack_path_for "$platform" "$workflow")"
        printf '%s\t%s\t%s\t%s\n' "$profile_name" "$platform" "$(get_k6_workflow_name "$workflow")" "$stack"
      done
    done
  done < <(resolve_profiles)
}

main() {
  cd "$ROOT_DIR"

  ensure_dependencies
  ensure_benchmark_envs
  ensure_benchmark_configs

  if bool_true "$DRY_RUN"; then
    print_benchmark_configs
    return
  fi


  trap 'handle_interrupt INT' INT
  trap 'handle_interrupt TERM' TERM
  trap on_exit EXIT

  log "initializing Terraform"
  terraform_init
  write_manifest

  local profile platform workflow run_failed
  while IFS= read -r profile; do
    for platform in $PLATFORMS; do
      for workflow in $WORKFLOWS; do
        run_failed="false"
        if ! run_one "$profile" "$platform" "$workflow"; then
          run_failed="true"
          CURRENT_RUN_FAILED="true"
          log "run failed: $(profile_name_for "$profile")/$platform/$(get_k6_workflow_name "$workflow")"
          if [[ -n "$CURRENT_RUN_DIR" ]]; then
            if [[ "$INTERRUPTED" == "true" ]]; then
              write_status "$CURRENT_RUN_DIR/status.json" "interrupted" "benchmark interrupted" || true
            else
              write_status "$CURRENT_RUN_DIR/status.json" "failed" "see run logs for details" || true
            fi
          fi
          cleanup_current_infra
        fi

        if [[ "$INTERRUPTED" == "true" ]]; then
          EXIT_STATUS=130
          fatal "stopping after interrupt"
        fi

        if [[ "$run_failed" == "true" ]] && ! bool_true "$CONTINUE_ON_ERROR"; then
          fatal "stopping after failed run"
        fi
      done
    done
  done < <(resolve_profiles)
}

main "$@"
