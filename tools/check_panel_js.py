#!/usr/bin/env python3
"""把面板首页里的内联 <script> 抽出来，交给 node 做语法检查。

为什么需要它：面板前端是一个上千行的模板字符串，Go 编译器完全看不见其中的
JS。曾经出现过「改模板时误删一个函数头」→ 整页语法错误 → 白屏，而所有
Go 单元测试仍然全绿（测试只断言字符串片段，不做语法分析）。这个脚本补上
最后一道闸门。

用法：
    python3 tools/check_panel_js.py /tmp/page.html
退出码 0 = 语法通过；非 0 = 有问题（并打印原因）。
"""
from __future__ import annotations

import re
import subprocess
import sys
import tempfile


def main() -> int:
    if len(sys.argv) < 2:
        print("用法: check_panel_js.py <渲染后的 index.html>", file=sys.stderr)
        return 2

    html = open(sys.argv[1], encoding="utf-8").read()
    m = re.search(r"<script>(.*)</script>", html, re.S)
    if not m:
        print("页面里找不到内联 <script>", file=sys.stderr)
        return 1

    js = m.group(1)
    print("内联脚本 %d 行 / %d 字节" % (js.count("\n"), len(js)))

    with tempfile.NamedTemporaryFile("w", suffix=".js", delete=False, encoding="utf-8") as f:
        f.write(js)
        path = f.name

    r = subprocess.run(["node", "--check", path], capture_output=True, text=True)
    if r.returncode != 0:
        print("内联 JS 语法错误：", file=sys.stderr)
        # node 的报错里带临时路径，换成好读的提示
        print(r.stderr.replace(path, "面板内联脚本"), file=sys.stderr)
        return 1

    print("内联 JS 语法检查通过")
    return 0


if __name__ == "__main__":
    sys.exit(main())
