#!/bin/bash
# llm-smoke.sh — LLM smoke test via stdio transport
# Usage: ./scripts/llm-smoke.sh [binary_path]
# Default binary: ./dolphin

set -euo pipefail

BIN="${1:-./dolphin}"
BIN=$(realpath "$BIN")

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

[ -x "$BIN" ] || fail "binary not found: $BIN (build it first: go build -o $BIN .)"

echo "=== LLM Smoke Test ==="
echo "  Binary: $BIN"

# Ensure first-run marker so career prompt is skipped
mkdir -p "${HOME}/.dolphin"
touch "${HOME}/.dolphin/first-run"

# Use a temp dir with config symlink so dolphin finds .dolphin/config.yaml
SMOKE_DIR=$(mktemp -d /tmp/dolphin-llm-smoke-XXXXXX)
trap 'rm -rf "$SMOKE_DIR"' EXIT
ln -sf "$(realpath .dolphin)" "$SMOKE_DIR/.dolphin"

# ── Test 1: valid key — LLM responds correctly ──────────────
echo "=== Test 1: valid key — LLM responds ==="
OUTPUT=$(cd "$SMOKE_DIR" && echo "abc 第一个字是什么？只回答一个字" | timeout 120 "$BIN" 2>&1 || true)

if echo "$OUTPUT" | grep -q "a"; then
	echo "  ✓ LLM returned 'a'"
elif echo "$OUTPUT" | grep -qiE "(error|fail|unable|unauthorized|rate limit)"; then
	echo "FAIL: LLM error" >&2
	echo "$OUTPUT" | grep -iE "(error|fail|unable|unauthorized|rate limit)" >&2
	exit 1
else
	echo "FAIL: unexpected response" >&2
	echo "--- output (last 10 lines) ---" >&2
	echo "$OUTPUT" | tail -10 >&2
	echo "---" >&2
	exit 1
fi

# ── Test 2: invalid key — proper error message ──────────────
echo "=== Test 2: invalid key — error message ==="
# Create a temp config with a bad key (env var doesn't override provider keys)
BAD_CFG_DIR=$(mktemp -d /tmp/dolphin-badkey-XXXXXX)
trap 'rm -rf "$SMOKE_DIR" "$BAD_CFG_DIR"' EXIT
BASE_URL=$(grep '^  base_url' .dolphin/config.yaml | head -1 | awk '{print $2}')
MODEL=$(grep '^  model' .dolphin/config.yaml | head -1 | awk '{print $2}')
cat > "$BAD_CFG_DIR/config.yaml" <<YAML
llm:
  providers:
    - name: test
      type: openai
      api_key: "sk-bad-invalid-key"
      base_url: $BASE_URL
      model: $MODEL
  type: openai
  base_url: $BASE_URL
  model: $MODEL
  api_key: "sk-bad-invalid-key"
  max_tokens: 10
transport:
  stdio:
    enabled: true
YAML
BAD_OUTPUT=$(echo "hi" | timeout 60 "$BIN" --config "$BAD_CFG_DIR/config.yaml" 2>&1 || true)

if echo "$BAD_OUTPUT" | grep -qiE "(unauthorized|401|authentication|invalid api key|auth failed|incorrect api key|forbidden|auth error)"; then
	echo "  ✓ shows auth error with bad key"
else
	echo "FAIL: expected auth error with bad key" >&2
	echo "--- output (last 10 lines) ---" >&2
	echo "$BAD_OUTPUT" | tail -10 >&2
	echo "---" >&2
	exit 1
fi

echo ""
echo "=== All LLM smoke tests passed ==="
