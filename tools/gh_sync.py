#!/usr/bin/env python3
"""走 REST API 把本地仓库同步到 GitHub（沙箱里 git 协议不通时用）。

用法：
    GH_TOKEN=github_pat_xxx python3 tools/gh_sync.py --repo OWNER/NAME [--dry-run]

设计要点（每条都是真踩过的坑）：
  1. 不用 git push —— 沙箱里 github.com 的 git 端点会静默超时。
  2. 用 blob sha 比对本地与远端，而不是 git status / git diff ——
     后者受本地提交历史干扰，会误判「有改动」。
  3. 提交走 Git Data API（tree + commit 一次提交多文件），
     相比 PUT /contents 逐个文件提交：不会 409，只产生一个 commit。
  4. 令牌只从环境变量读，绝不写进文件、绝不进 git。
"""
from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request

API = "https://api.github.com"


def token() -> str:
    t = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if not t:
        sys.exit("缺少 GH_TOKEN 环境变量")
    if "…" in t or t.endswith("xxx"):
        sys.exit("GH_TOKEN 看起来被截断了，请从原始来源复制完整串")
    return t


def call(method: str, path: str, payload: dict | None = None) -> dict:
    url = path if path.startswith("https://") else API + path
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", f"Bearer {token()}")
    req.add_header("Accept", "application/vnd.github+json")
    req.add_header("X-GitHub-Api-Version", "2022-11-28")
    if data:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            body = r.read().decode()
            return json.loads(body) if body else {}
    except urllib.error.HTTPError as e:
        detail = e.read().decode()[:400]
        sys.exit(f"{method} {url} -> {e.code}: {detail}")


def blob_sha(data: bytes) -> str:
    """与 git 内部一致的对象哈希：sha1("blob <len>\\0" + content)。"""
    h = hashlib.sha1()
    h.update(b"blob %d\0" % len(data))
    h.update(data)
    return h.hexdigest()


def local_files(root: str) -> tuple[dict[str, tuple[bytes, str]], int]:
    """列出要同步的文件，返回 {路径: (内容, git mode)} 与未跟踪文件数。

    两件事必须一起做，少一个都会静默出错：

    1. **mode 从 git 读，不靠后缀猜**。早先按 `.sh` 结尾判 100755，
       结果非 `.sh` 的可执行文件（比如无后缀的构建脚本）会被降成 100644，
       推上去就没有可执行位了，且 git status 看不出来。
    2. **未跟踪但未被忽略的文件也要算**。沙箱里新写的文件常常还没 `git add`，
       只列 `ls-files` 会漏推——提交成功、CI 通过、文件却不在，最难查。
       这类文件的 mode 无从得知，按后缀给默认值。
    """
    files: dict[str, tuple[bytes, str]] = {}

    tracked = subprocess.run(
        ["git", "-C", root, "ls-files", "-s", "-z"],
        capture_output=True, check=True,
    ).stdout.split(b"\0")
    for raw in tracked:
        if not raw:
            continue
        meta, _, rel = raw.partition(b"\t")
        mode = meta.split(b" ")[0].decode()
        path = rel.decode()
        with open(os.path.join(root, path), "rb") as f:
            files[path] = (f.read(), mode)

    untracked = subprocess.run(
        ["git", "-C", root, "ls-files", "-o", "-z", "--exclude-standard"],
        capture_output=True, check=True,
    ).stdout.split(b"\0")
    added = 0
    for raw in untracked:
        if not raw:
            continue
        path = raw.decode()
        full = os.path.join(root, path)
        if os.path.islink(full) or not os.path.isfile(full):
            continue
        with open(full, "rb") as f:
            files[path] = (f.read(), "100755" if path.endswith(".sh") else "100644")
        added += 1

    return files, added


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True, help="OWNER/NAME")
    ap.add_argument("--root", default=".", help="本地仓库根目录")
    ap.add_argument("--branch", default="main")
    ap.add_argument("--message", default="sync from sandbox via REST API")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    root = os.path.abspath(args.root)
    local, untracked = local_files(root)
    print(f"本地文件 {len(local)} 个" + (f"（其中 {untracked} 个未 git add）" if untracked else ""))

    # 远端当前 tree（仓库为空时 404，按空树处理）
    try:
        head = call("GET", f"/repos/{args.repo}/git/ref/heads/{args.branch}")
        base_commit = head["object"]["sha"]
        commit = call("GET", f"/repos/{args.repo}/git/commits/{base_commit}")
        base_tree = commit["tree"]["sha"]
        remote = {
            i["path"]: i["sha"]
            for i in call("GET", f"/repos/{args.repo}/git/trees/{base_tree}?recursive=1")["tree"]
            if i["type"] == "blob"
        }
        print(f"远端文件 {len(remote)} 个，base commit {base_commit[:8]}")
    except SystemExit:
        print("远端为空分支，按首次提交处理")
        base_commit, remote = None, {}

    changed = {p: v for p, v in local.items() if remote.get(p) != blob_sha(v[0])}
    deleted = [p for p in remote if p not in local]
    print(f"需更新 {len(changed)} 个，需删除 {len(deleted)} 个")

    if not changed and not deleted:
        print("已经一致，无需提交")
        return
    for p in changed:
        print(f"  M {p}")
    for p in deleted:
        print(f"  D {p}")
    if args.dry_run:
        print("--dry-run，不提交")
        return

    # 一次性建新 tree：已有文件用 base_tree 的 mode/type，新文件补上
    entries = []
    for path, (content, mode) in changed.items():
        blob = call("POST", f"/repos/{args.repo}/git/blobs", {
            "content": base64.b64encode(content).decode(),
            "encoding": "base64",
        })
        entries.append({
            "path": path,
            "mode": mode,
            "type": "blob",
            "sha": blob["sha"],
        })
    for path in deleted:
        entries.append({"path": path, "mode": "100644", "type": "blob", "sha": None})

    tree: dict = {"tree": entries}
    if base_commit:
        tree["base_tree"] = base_tree
    new_tree = call("POST", f"/repos/{args.repo}/git/trees", tree)
    print(f"新 tree {new_tree['sha'][:8]}")

    new_commit = call("POST", f"/repos/{args.repo}/git/commits", {
        "message": args.message,
        "tree": new_tree["sha"],
        "parents": [base_commit] if base_commit else [],
    })
    print(f"新 commit {new_commit['sha'][:8]}")

    if base_commit:
        call("PATCH", f"/repos/{args.repo}/git/refs/heads/{args.branch}", {
            "sha": new_commit["sha"], "force": False,
        })
    else:
        call("POST", f"/repos/{args.repo}/git/refs", {
            "ref": f"refs/heads/{args.branch}", "sha": new_commit["sha"],
        })
    print(f"已推送到 {args.repo}@{args.branch}")


if __name__ == "__main__":
    main()
