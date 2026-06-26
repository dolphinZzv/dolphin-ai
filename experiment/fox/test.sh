#!/bin/bash
# Fox — 运行全部测试
# 用法: bash test.sh
set -euo pipefail
cd "$(dirname "$0")"

echo "========================================="
echo "  Fox — Full Test Suite"
echo "========================================="

# ─── Go server tests ────────────────────────────────
echo ""
echo "── Go behavior-server tests ──"
(cd cmd/behavior-server && go test -v -count=1 ./...) || exit 1

# ─── JS extension validation ───────────────────────
echo ""
echo "── JS extension validation ──"
node extension/test/validate.mjs || exit 1

echo ""
echo "========================================="
echo "  ALL TESTS PASSED"
echo "========================================="
