#!/usr/bin/env bash
# Memory math for the local model, a recommendation, and a tokens/s measurement.
# Recommends a size class, never a hardcoded model name: the generation moves.
set -uo pipefail
cd "$(dirname "$0")/.."
source versions.env
PROFILE=${PROFILE:-dev}

total_gb=$(( $(sysctl -n hw.memsize) / 1024 / 1024 / 1024 ))
case "$PROFILE" in
  dev)   vm=5 ;;
  e2e)   vm=10 ;;
  bench) vm=4 ;;
  *) echo "doctor-ai: unknown PROFILE '$PROFILE'" >&2; exit 2 ;;
esac
reserve=5
budget=$(( total_gb - vm - reserve ))

echo "memory: ${total_gb} GB physical − ${vm} GB VM (${PROFILE}) − ${reserve} GB macOS = ${budget} GB for the model"
if [[ "$PROFILE" == "e2e" ]]; then
  echo "doctor-ai: the e2e profile never loads a model. Nothing to recommend."
  exit 0
fi
if (( budget < 5 )); then
  echo "doctor-ai: ${budget} GB is not enough for a useful model. Use CLUSTER_AI=off, or point"
  echo "           CLUSTER_AI_URL at a model on another machine."
  exit 1
fi

if (( budget >= 40 )); then       class="a large MoE at Q4, or a 32B dense at Q8"
elif (( budget >= 18 )); then     class="a 27-32B dense at Q4 (~17-20 GB)"
elif (( budget >= 12 )); then     class="a ~35B-A3B MoE at Q4 (mmap; only ~3B active per token)"
elif (( budget >= 8 )); then      class="a ~14B dense at Q4_K_M (~9 GB)"
else                              class="a ~9B dense at Q4 (~6.6 GB)"
fi
echo "recommended: ${class}"

url=${CLUSTER_AI_URL:-http://127.0.0.1:1234/v1}
if ! curl -sS -m 3 "${url}/models" >/dev/null 2>&1; then
  echo "endpoint: ${url} is not answering. Start the runtime (mlx_lm.server, LM Studio, Ollama),"
  echo "          pick a model in the class above, then re-run: make doctor-ai"
  exit 1
fi
echo "endpoint: ${url}"
curl -sS -m 5 "${url}/models" | jq -r '.data[].id' | sed 's/^/  model: /'

model=${CLUSTER_AI_MODEL:-$(curl -sS -m 5 "${url}/models" | jq -r '.data[0].id')}
start=$(python3 -c 'import time;print(time.time())')
out=$(curl -sS -m 120 "${url}/chat/completions" -H 'content-type: application/json' -d "$(jq -nc \
  --arg m "$model" '{model:$m,temperature:0,max_tokens:200,messages:[{role:"user",content:"Explain in 200 tokens what a Kubernetes control plane does."}]}')")
end=$(python3 -c 'import time;print(time.time())')
tokens=$(jq -r '.usage.completion_tokens // 0' <<<"$out")
tps=$(python3 -c "print(f'{$tokens/max($end-$start,0.001):.1f}')")
echo "throughput: ${tokens} tokens in $(python3 -c "print(f'{$end-$start:.1f}')")s = ${tps} tok/s (model ${model})"
if (( $(python3 -c "print(1 if $tps < 8 else 0)") )); then
  echo "doctor-ai: under 8 tok/s. A slow --explain is worse than the runbook alone; go one size down."
  exit 1
fi
echo
echo "Record the pick in versions.env (CLUSTER_AI_MODEL, CLUSTER_AI_QUANT, CLUSTER_AI_SHA256,"
echo "CLUSTER_AI_RUNTIME) and the memory math in DECISIONS.md."
