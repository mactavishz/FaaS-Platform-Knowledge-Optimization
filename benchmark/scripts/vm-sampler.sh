#!/usr/bin/env bash

# Sample whole-VM CPU and memory usage at a fixed interval.
#
# Runs on the benchmark VM (pushed and started by benchmark/run.sh) and appends
# one CSV row per tick, so a kill at any moment leaves a valid file. CPU is the
# non-idle share of /proc/stat jiffies since the previous tick (idle + iowait
# count as idle), matching the utilization fraction GCE reports. Memory used is
# MemTotal - MemAvailable from /proc/meminfo.
#
# Usage: vm-sampler.sh [interval_seconds] [output_csv]

set -u

INTERVAL="${1:-5}"
OUTPUT="${2:-/tmp/vm-samples.csv}"

read_cpu_counters() {
  local _label user nice system idle iowait irq softirq steal _rest
  read -r _label user nice system idle iowait irq softirq steal _rest </proc/stat
  cpu_total=$((user + nice + system + idle + iowait + irq + softirq + steal))
  cpu_idle=$((idle + iowait))
}

if [ ! -s "$OUTPUT" ]; then
  printf 'timestamp,cpu_pct,mem_used_pct,mem_used_bytes\n' >"$OUTPUT"
fi

read_cpu_counters
prev_total=$cpu_total
prev_idle=$cpu_idle
next=$(date +%s)

while true; do
  next=$((next + INTERVAL))
  now=$(date +%s)
  if [ $((next - now)) -gt 0 ]; then
    sleep $((next - now))
  fi

  read_cpu_counters
  timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  total_delta=$((cpu_total - prev_total))
  idle_delta=$((cpu_idle - prev_idle))
  prev_total=$cpu_total
  prev_idle=$cpu_idle

  cpu_pct="$(awk -v t="$total_delta" -v i="$idle_delta" \
    'BEGIN { printf "%.4f", (t > 0 ? (t - i) * 100 / t : 0) }')"
  mem_cells="$(awk '
    /^MemTotal:/ { total = $2 }
    /^MemAvailable:/ { avail = $2 }
    END {
      used = total - avail
      printf "%.4f,%.0f", (total > 0 ? used * 100 / total : 0), used * 1024
    }' /proc/meminfo)"

  printf '%s,%s,%s\n' "$timestamp" "$cpu_pct" "$mem_cells" >>"$OUTPUT"
done
