#!/usr/bin/env bash
# 推送本地改动到 GitHub（沙箱里 git 协议不通，只能走 Git Data API）。
#
# 用法：GITHUB_TOKEN=xxx tools/push-via-api.sh [--dry-run]
#       MSG="feat: xxx" GITHUB_TOKEN=xxx tools/push-via-api.sh
#
# 这个脚本原来自己实现了一遍「逐文件建 blob → 建 tree → 移动 ref」，
# 有两个硬伤，现改为对 tools/gh_sync.py 的薄封装（保留文件名，文档与历史习惯都在用）：
#
#   1. 建 tree 时**没带 base_tree**，等于按本地文件整棵树重建。
#      远端有、本地没有的文件（别人刚推的、或本地漏拉的）会被静默删除，
#      而且 commit 看起来完全正常。这类事故在公开仓库里很难回滚。
#   2. 每次重传全部 blob：90 个文件 = 90 次请求，慢且浪费额度。
#      gh_sync 只传 blob sha 有差异的文件。
#
# 语义变化：**不再要求先本地 commit**，直接同步工作区已跟踪 + 未忽略的文件。
# 推送前请先跑 `python3 tools/check_secrets.py .`。
set -euo pipefail

: "${GITHUB_TOKEN:=${GH_TOKEN:-}}"
: "${GITHUB_TOKEN:?需要 GITHUB_TOKEN 或 GH_TOKEN 环境变量}"
REPO="${REPO:-ElyYinLi/aipoints-mobile}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MSG="${MSG:-sync from sandbox via REST API}"

args=(--repo "$REPO" --root "$ROOT" --message "$MSG")
if [ "${1:-}" = "--dry-run" ]; then
  args+=(--dry-run)
fi

GITHUB_TOKEN="$GITHUB_TOKEN" python3 "$ROOT/tools/gh_sync.py" "${args[@]}"
