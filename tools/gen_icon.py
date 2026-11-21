#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""生成 Points App 的 iOS 全尺寸图标。

设计（V4「错位呼应」）：深蓝紫对角渐变底 + 白色空心 spark 双星。
**图形坐标与描边宽度保持不变** —— 用户明确拒绝改描边，这里只做规范修正：
  1. 满幅方形，不预烘焙圆角（iOS 自己按 ≈22.37% 裁）
  2. 输出 RGB（无 alpha 通道）—— App Store 强制要求，透明区会渲染成纯黑
  3. 输出 iOS 全套 13 个尺寸 + 完整 18 条 Contents.json

和旧流程的区别：以前用 `sips -z` 现场缩放，只有 4 个平铺 PNG；
现在一次性生成完整 .appiconset，actool 一次编译到位。

用法：
    python3 tools/gen_icon.py            # 生成到 assets/AppIcon.appiconset
    python3 tools/gen_icon.py --check    # 只校验现有产物是否合规
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "assets" / "AppIcon.appiconset"

# 画布：2048 超采样后再降采样，边缘更干净（对齐 aipy-checkin 的做法）
S = 2048
SCALE = S / 1024.0

# 三色对角渐变（与 icons.go / app-icon.svg 完全一致）
C0 = (0x4F, 0x6B, 0xFF)
C1 = (0x7B, 0x4D, 0xFF)
C2 = (0xC2, 0x4D, 0xFF)

# V4 双星：主星 R=220 r=60 @ (455,415)，副星 R=112 r=30 @ (710,735)
#
# 0.7.1 描边减细：主 48→30、副 28→17。
# 依据是同尺寸逐像素比对 aipy-checkin：白色像素占比 8.03% → 5.23%
# （参考实现是 3.96%）。原来的 48 太粗，主星四角之间的凹口被填平，
# 星形退化成一团；减细后轮廓才立得住。小星同步按同比例（17/30）减细。
STARS = [
    # (cx, cy, R, r, stroke_width_1024)
    (455.0, 415.0, 220.0, 60.0, 30.0),
    (710.0, 735.0, 112.0, 30.0, 17.0),
]


def star_points(cx, cy, R, r, n=8):
    """四角星：8 个顶点交替 R / r。起始角取正上方，与 SVG path 一致。"""
    import math

    pts = []
    for i in range(n):
        # SVG 里 M455,195 是从上顶点开始顺时针；这里等价用 -90° 起算
        ang = math.radians(-90 + i * 45)
        rad = R if i % 2 == 0 else r
        pts.append((cx + rad * math.cos(ang), cy + rad * math.sin(ang)))
    return pts


def build_master() -> Image.Image:
    """2048 画布渲染 → LANCZOS 降采样到 1024 → 转 RGB。"""
    # 对角渐变：t = (x+y)/(2S)，与 SVG 的 x1,y1 -> x2,y2 对角向量一致。
    # 逐像素用 numpy 算，2048² 也就几十毫秒；用 PIL 逐行画会退化成水平渐变。
    import numpy as np

    yy, xx = np.mgrid[0:S, 0:S].astype(np.float32)
    t = np.clip((xx + yy) / (2.0 * S), 0, 1)
    a = np.array(C0, dtype=np.float32)
    b = np.array(C1, dtype=np.float32)
    c = np.array(C2, dtype=np.float32)
    k1 = np.clip(t / 0.55, 0, 1)[..., None]
    k2 = np.clip((t - 0.55) / 0.45, 0, 1)[..., None]
    col = np.where(t[..., None] < 0.55, a + (b - a) * k1, b + (c - b) * k2)
    img = Image.fromarray(np.clip(col, 0, 255).astype(np.uint8), "RGB")
    dr = ImageDraw.Draw(img)

    # 双星：圆头描边，用 line + 端点圆模拟 stroke-linejoin/linecap round
    for cx, cy, R, r, w in STARS:
        pts = star_points(cx * SCALE, cy * SCALE, R * SCALE, r * SCALE)
        wpx = max(1, int(round(w * SCALE)))
        dr.line(pts + [pts[0]], fill=(255, 255, 255), width=wpx, joint="curve")
        # 顶点补圆头：line 的 joint=curve 只处理中间拐点，闭合处首尾要自己补
        for p in pts:
            rad = wpx / 2.0
            dr.ellipse([p[0] - rad, p[1] - rad, p[0] + rad, p[1] + rad], fill=(255, 255, 255))

    # 降采样 + 去 alpha
    master = img.resize((1024, 1024), Image.LANCZOS)
    return master.convert("RGB")


# iOS 需要的全部尺寸（(idiom, scale, size) -> px）
ENTRIES = [
    ("iphone", "2x", "20x20", 40),
    ("iphone", "3x", "20x20", 60),
    ("iphone", "2x", "29x29", 58),
    ("iphone", "3x", "29x29", 87),
    ("iphone", "2x", "40x40", 80),
    ("iphone", "3x", "40x40", 120),
    ("iphone", "2x", "60x60", 120),
    ("iphone", "3x", "60x60", 180),
    ("ipad", "1x", "20x20", 20),
    ("ipad", "2x", "20x20", 40),
    ("ipad", "1x", "29x29", 29),
    ("ipad", "2x", "29x29", 58),
    ("ipad", "1x", "40x40", 40),
    ("ipad", "2x", "40x40", 80),
    ("ipad", "1x", "76x76", 76),
    ("ipad", "2x", "76x76", 152),
    ("ipad", "2x", "83.5x83.5", 167),
    ("ios-marketing", "1x", "1024x1024", 1024),
]


def generate() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    master = build_master()
    master.save(ROOT / "assets" / "app-icon-1024.png", "PNG", optimize=True)

    made: dict[int, str] = {}
    images = []
    for idiom, scale, size, px in ENTRIES:
        if px not in made:
            name = f"icon-{px}.png"
            # 1024 直接存 master；其余用 LANCZOS
            out = master if px == 1024 else master.resize((px, px), Image.LANCZOS)
            out.convert("RGB").save(OUT / name, "PNG", optimize=True)
            made[px] = name
        images.append({"filename": made[px], "idiom": idiom, "scale": scale, "size": size})

    (OUT / "Contents.json").write_text(
        json.dumps({"images": images, "info": {"author": "points", "version": 1}},
                   indent=2, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )
    print(f"已生成 {len(made)} 个尺寸 / {len(images)} 条目录 → {OUT}")

    # 同步两张给 Android / 文档用的单图
    master.resize((512, 512), Image.LANCZOS).save(
        ROOT / "assets" / "app-icon-512.png", "PNG", optimize=True)
    master.resize((540, 540), Image.LANCZOS).save(
        ROOT / "assets" / "app-icon-small.png", "PNG", optimize=True)
    print("已同步 app-icon-1024.png / app-icon-512.png / app-icon-small.png")


def check() -> int:
    """校验产物合规：无 alpha、满幅（四角不透明）、尺寸齐全。"""
    bad = 0
    if not OUT.exists():
        print(f"✗ 缺少 {OUT}")
        return 1

    for idiom, scale, size, px in ENTRIES:
        f = OUT / f"icon-{px}.png"
        if not f.exists():
            print(f"✗ 缺尺寸 {px}px ({idiom} {size}@{scale})")
            bad += 1
            continue
        im = Image.open(f)
        if im.mode != "RGB":
            print(f"✗ {f.name} 模式是 {im.mode}，必须是 RGB（无 alpha）")
            bad += 1
        if im.size != (px, px):
            print(f"✗ {f.name} 尺寸 {im.size}，应为 {(px, px)}")
            bad += 1
        # 四角必须不透明（不能是预烘焙圆角留下的透明/黑色）
        corner = im.convert("RGB").getpixel((1, 1))
        if sum(corner) < 60:
            print(f"✗ {f.name} 左上角 {corner} 过暗，疑似预烘焙圆角")
            bad += 1

    cj = OUT / "Contents.json"
    data = json.loads(cj.read_text(encoding="utf-8"))
    if len(data.get("images", [])) != len(ENTRIES):
        print(f"✗ Contents.json 条目 {len(data.get('images', []))}，应为 {len(ENTRIES)}")
        bad += 1

    if bad == 0:
        print(f"✓ 图标产物全部合规（{len(ENTRIES)} 条）")
    return bad


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true", help="只校验，不生成")
    args = ap.parse_args()
    sys.exit(check() if args.check else (generate() or 0))
