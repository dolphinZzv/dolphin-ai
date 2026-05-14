#!/bin/bash
# docs-smoke.sh — Documentation validation
# Validates config examples, internal links, and doc consistency.

set -euo pipefail

ROOT=$(realpath "$(dirname "$0")/..")
cd "$ROOT"

fail() {
	echo "FAIL: $*" >&2
	exit 1
}

PASS=0
FAIL=0

check() {
	local name="$1"; shift
	if eval "$@"; then
		echo "  ✓ $name"
		PASS=$((PASS + 1))
	else
		echo "  ✗ $name"
		FAIL=$((FAIL + 1))
	fi
}

echo "=== Docs Smoke Test ==="
echo ""

# ── 1. Config examples are valid YAML ──────────────────────
echo "--- Config validation ---"
check "docs/en/config.example.yaml is valid YAML" \
	"python3 -c 'import yaml; yaml.safe_load(open(\"docs/en/config.example.yaml\"))'"
check "docs/zh/config.example.zh.yaml is valid YAML" \
	"python3 -c 'import yaml; yaml.safe_load(open(\"docs/zh/config.example.zh.yaml\"))'"

# Check that the actual config can be loaded as valid YAML
check ".dolphin/config.yaml is valid YAML" \
	"python3 -c 'import yaml; yaml.safe_load(open(\".dolphin/config.yaml\"))'"

# ── 2. DeepSeek config consistency ─────────────────────────
echo ""
echo "--- DeepSeek config consistency ---"
# Chinese config example should reference deepseek as default
check "zh config default model is deepseek-v4-flash" \
	"grep -q 'deepseek-v4-flash' docs/zh/config.example.zh.yaml"
check "zh config default base_url has deepseek" \
	"grep -q 'api.deepseek.com' docs/zh/config.example.zh.yaml"

# README deepseek env vars should match actual config
ACTUAL_BASE_URL=$(grep '^  base_url' .dolphin/config.yaml | awk '{print $2}')
ACTUAL_MODEL=$(grep '^  model' .dolphin/config.yaml | awk '{print $2}')
check "README.zh.md DZ_LLM_MODEL matches config" \
	"grep -q \"$ACTUAL_MODEL\" README.zh.md"
check "README.zh.md DZ_LLM_BASE_URL matches config" \
	"grep -q \"$ACTUAL_BASE_URL\" README.zh.md"

# ── 3. Internal links in markdown ──────────────────────────
echo ""
echo "--- Internal link validation ---"
BROKEN=0
for f in $(find docs/ design/ workflow/ -name '*.md'); do
	dir=$(dirname "$f")
	while IFS= read -r link; do
		[ -z "$link" ] && continue
		target="$dir/$link"
		target="${target%%#*}"
		[ -n "$target" ] && [ ! -f "$target" ] && echo "  Broken: $f -> $link" && BROKEN=1
	done < <(grep -oP '\]\(\K[^)]+' "$f" 2>/dev/null | grep -v '^http' | grep -v '^#')
done
if [ "$BROKEN" -eq 0 ]; then
	echo "  ✓ all internal links valid"
	PASS=$((PASS + 1))
else
	FAIL=$((FAIL + 1))
fi

# ── 4. README contains required sections ───────────────────
echo ""
echo "--- README structure ---"
check "README.md has install section" "grep -qi 'install' README.md"
check "README.md has config section" "grep -qi 'config' README.md"
check "README.zh.md has install section" "grep -qi '安装\|install' README.zh.md"
check "README.zh.md has config section" "grep -qi '配置\|config' README.zh.md"

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
[ "$FAIL" -eq 0 ] || exit 1
