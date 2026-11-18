#!/usr/bin/env python3
"""上传/发版前的硬闸：扫描待提交内容里的令牌、私钥。

不靠人记，靠代码拦。把它挂在 CI 的 core-ci 里，任何一次含密钥的提交都会被拒。

用法：
    python3 tools/check_secrets.py .        # 扫描工作区
    python3 tools/check_secrets.py x.zip    # 扫描 zip 包内文件名
"""
from __future__ import annotations

import re
import sys
import zipfile
from pathlib import Path

# 只匹配真实形态的密钥，避免误报（比如文档里写 "github_pat_xxx" 占位符）
PATTERNS = [
    (re.compile(rb"github_pat_[A-Za-z0-9_]{20,}"), "GitHub fine-grained PAT"),
    (re.compile(rb"ghp_[A-Za-z0-9]{30,}"), "GitHub classic PAT"),
    (re.compile(rb"-----BEGIN [A-Z ]*PRIVATE KEY-----"), "私钥"),
    (re.compile(rb"AKIA[0-9A-Z]{16}"), "AWS Access Key"),
]

SECRET_SUFFIX = {".jks", ".keystore", ".p12", ".pfx", ".mobileprovision", ".pem", ".key"}

SKIP_DIRS = {".git", "node_modules", "build", "dist", "__pycache__", ".venv"}


def scan_bytes(data: bytes, where: str, hits: list[str]) -> None:
    for pat, name in PATTERNS:
        for m in pat.finditer(data):
            frag = m.group(0)[:16].decode("utf-8", "replace")
            hits.append(f"{where}: 命中 {name} ({frag}…)")


def scan_zip(p: Path, hits: list[str]) -> None:
    with zipfile.ZipFile(p) as z:
        for n in z.namelist():
            low = n.lower()
            if any(low.endswith(s) for s in SECRET_SUFFIX) or "/signing/" in low:
                hits.append(f"{p.name}: 内含密钥文件 {n}")
            if low.endswith((".txt", ".json", ".yml", ".yaml", ".sh", ".py", ".md")):
                try:
                    scan_bytes(z.read(n), f"{p.name}:{n}", hits)
                except Exception:
                    pass


def main() -> None:
    targets = [Path(a) for a in (sys.argv[1:] or ["."])]
    hits: list[str] = []

    for t in targets:
        if t.suffix in {".zip", ".apk", ".ipa", ".aar"} and t.is_file():
            scan_zip(t, hits)
            continue
        if t.is_file():
            scan_bytes(t.read_bytes(), str(t), hits)
            continue
        for f in t.rglob("*"):
            if any(part in SKIP_DIRS for part in f.parts):
                continue
            if f.is_file():
                if f.suffix in SECRET_SUFFIX:
                    hits.append(f"{f}: 密钥类文件不应入库")
                    continue
                if f.stat().st_size > 2_000_000:
                    continue
                try:
                    scan_bytes(f.read_bytes(), str(f), hits)
                except Exception:
                    pass

    if hits:
        print("[拒绝] 发现疑似密钥：")
        for h in hits:
            print("  " + h)
        sys.exit(1)
    print(f"[通过] 扫描 {len(targets)} 个目标，未发现密钥")


if __name__ == "__main__":
    main()
