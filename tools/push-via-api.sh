#!/usr/bin/env bash
# 推送本地 main 到 GitHub（沙箱里 git 协议不通，只能走 Git Data API）。
#
# 用法：tools/push-via-api.sh
# 令牌从环境变量 GITHUB_TOKEN 读，绝不写进仓库。
set -euo pipefail

: "${GITHUB_TOKEN:?需要 GITHUB_TOKEN}"
REPO="${REPO:-ElyYinLi/aipoints-mobile}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

GITHUB_TOKEN="$GITHUB_TOKEN" REPO="$REPO" ROOT="$ROOT" python3 - <<'PY'
import base64, json, os, subprocess, urllib.request

T = os.environ["GITHUB_TOKEN"]; REPO = os.environ["REPO"]; ROOT = os.environ["ROOT"]

def api(method, path, data=None):
    req = urllib.request.Request(
        "https://api.github.com" + path, method=method,
        data=json.dumps(data).encode() if data else None,
        headers={"Authorization": "Bearer " + T,
                 "Accept": "application/vnd.github+json", "User-Agent": "push"})
    with urllib.request.urlopen(req) as r:
        b = r.read()
        return json.loads(b) if b else {}

git = lambda *a: subprocess.check_output(["git"] + list(a), cwd=ROOT).decode()

# -c core.quotepath=false：否则中文文件名会被转义成 \351 形式，open 会失败
files = subprocess.check_output(
    ["git", "-c", "core.quotepath=false", "ls-tree", "-r", "--name-only", "HEAD"],
    cwd=ROOT).decode().splitlines()

tree = []
for f in files:
    with open(os.path.join(ROOT, f), "rb") as fh:
        content = fh.read()
    blob = api("POST", f"/repos/{REPO}/git/blobs",
               {"content": base64.b64encode(content).decode(), "encoding": "base64"})
    tree.append({"path": f, "type": "blob", "sha": blob["sha"],
                 "mode": "100755" if f.endswith(".sh") else "100644"})

base = api("GET", f"/repos/{REPO}/git/ref/heads/main")["object"]["sha"]
new_tree = api("POST", f"/repos/{REPO}/git/trees", {"tree": tree})
msg = git("log", "-1", "--pretty=%B").strip()
commit = api("POST", f"/repos/{REPO}/git/commits",
             {"message": msg, "tree": new_tree["sha"], "parents": [base]})
api("PATCH", f"/repos/{REPO}/git/refs/heads/main", {"sha": commit["sha"]})
print(f"pushed {commit['sha'][:8]}  ({len(files)} files)  {msg.splitlines()[0]}")
PY
