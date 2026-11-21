#!/usr/bin/env bash
# 构建 iOS 静态库（Mobile.xcframework）与可自签的 .app / .ipa。
#
# 只在 macOS 上可跑。CI 里对应 .github/workflows/build-ios.yml。
#
# 设计取舍（与常见方案对比）：
#   1. 【重点】不用 xcodebuild 编 Swift，而是 SwiftPM + `swift build`：
#      仓库里没有 .xcodeproj，xcodebuild 需要工程文件；SwiftPM 只需要一个
#      Package.swift，CI 上零依赖。之前直接 `swiftc -framework Mobile` 链接
#      静态 framework 会失败（静态库的 .swiftmodule 不被 -F 正确识别）。
#   2. gomobile bind 产出的是【静态库】而非 dylib —— 后者会被塞进 Frameworks/
#      目录，自签工具（SideStore / LiveContainer 等）会报 `Invalid mach-o file!`。
#   3. 只编 arm64（真机），减小产物。
#   4. 用 ldid（Theos 自带）做 ad-hoc 签名并写入 entitlements；没有 ldid 时
#      退回 codesign --sign -。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CORE="$ROOT/core"
OUT="$ROOT/ios/build"
APP="$OUT/Points.app"
XCF="$OUT/Mobile.xcframework"
MIN_IOS="${MIN_IOS:-15.0}"
VERSION="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$ROOT/ios/App/Info.plist" 2>/dev/null || echo 0.0.0)"

mkdir -p "$OUT"
rm -rf "$APP" "$OUT/Payload"
mkdir -p "$APP"

# ---------------------------------------------------------------- 1. Go 引擎
echo "== 1/6 生成 Mobile.xcframework (ios/arm64) =="
export PATH="$PATH:$(go env GOPATH)/bin"
GP="$(go env GOPATH)"
if [ ! -d "$XCF" ]; then
  command -v gomobile >/dev/null || go install golang.org/x/mobile/cmd/gomobile@latest
  command -v gobind   >/dev/null || go install golang.org/x/mobile/cmd/gobind@latest
  ( cd "$CORE" && go build ./... )
  # bind 必须在 GOPATH 模式下跑：module 模式下 gomobile 会要求 core/go.mod
  # 里显式依赖 golang.org/x/mobile（`go get -tool`），而那个包要求 go>=1.26，
  # 会把引擎的 go 指令一起抬高。GOPATH 下用软链把仓库挂进去即可隔离。
  mkdir -p "$GP/src/github.com/ElyYinLi/aipoints-mobile"
  ln -sfn "$CORE" "$GP/src/github.com/ElyYinLi/aipoints-mobile/core"
  ( cd "$GP/src/github.com/ElyYinLi/aipoints-mobile/core" && \
    GO111MODULE=off gomobile bind \
      -target=ios \
      -iosversion="$MIN_IOS" \
      -ldflags "-s -w" \
      -o "$XCF" ./mobile )
fi
[ -d "$XCF/ios-arm64/Mobile.framework" ] || { echo "xcframework 结构异常"; find "$XCF" -maxdepth 3; exit 1; }

# ---------------------------------------------------------------- 2. Swift
echo "== 2/6 编译 Swift（xcrun swiftc，目标 ios/arm64）=="
# 为什么不用 `swift build --triple`：SwiftPM 的 cross-compile 只改 target triple、
# 不改 sysroot，编 UIKit 时报 "using sysroot for 'MacOSX' but targeting 'iPhone'"
# 和 "cannot load underlying module for 'UIKit'"。这里直接给 swiftc -sdk。
SDK="$(xcrun --sdk iphoneos --show-sdk-path)"
TARGET="arm64-apple-ios${MIN_IOS}"
SWIFT_SOURCES=$(find "$ROOT/ios/App/Sources" -name '*.swift' | sort)
BIN="$OUT/Points"

# shellcheck disable=SC2086
xcrun swiftc -sdk "$SDK" -target "$TARGET" \
  -parse-as-library -O -whole-module-optimization \
  -F "$XCF/ios-arm64" -framework Mobile \
  -framework SwiftUI -framework WebKit -framework Security -framework BackgroundTasks \
  -Xlinker -rpath -Xlinker /usr/lib/swift \
  $SWIFT_SOURCES \
  -o "$BIN" 2>&1 | tail -40

[ -f "$BIN" ] || { echo "Swift 编译未产出可执行文件"; find "$OUT" -maxdepth 2 -type f | head -20; exit 1; }

echo "== 3/6 组装 .app =="
cp "$BIN" "$APP/Points"
strip -x "$APP/Points" 2>/dev/null || true
cp "$ROOT/ios/App/Info.plist" "$APP/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $VERSION" "$APP/Info.plist" 2>/dev/null || true

echo "== 4/6 生成图标 =="
# 0.7.2：这一整块重做，因为 0.7.0/0.7.1 的写法让 iOS 拿到的是**错误尺寸**的图标。
#
# 病灶：旧脚本把 CFBundleIconFiles 写成 ["AppIcon60","AppIcon120","AppIcon180"]，
# 并在 .app 根目录放 AppIcon60.png(60) / AppIcon120.png(120) / AppIcon180.png(180)。
# 但 iOS 解析 CFBundleIconFiles 的规则是「基础名 + @2x/@3x」，即对 AppIcon60 只会找
#   AppIcon60.png(1x=60) / AppIcon60@2x.png(120) / AppIcon60@3x.png(180)
# 而 AppIcon120、AppIcon180 是**另外两个基础名**，系统匹配 AppIcon60 时根本不会看它们。
# 于是 @3x 屏（所有现代 iPhone）要 180px 图标，系统只找到 60px 的 AppIcon60.png，
# 放大 3 倍 → 无论源图多锐利，主屏上都是糊的。这才是「图标一直不清晰」的真因，
# 之前几轮所有描边/超采样的调整都是在错误的层面上打转。
#
# 现在两条路都铺对：
#   1. CFBundleIconName=AppIcon → 系统读 Assets.car（13 尺寸全套，actool 编译）；
#   2. 平铺文件按 name@2x/@3x 正确命名 → 自签重签工具破坏 Assets.car 时的兜底。
ICONSET="$ROOT/assets/AppIcon.appiconset"
if [ -f "$ICONSET/icon-1024.png" ]; then
  ASSETS="$OUT/Assets.xcassets"
  rm -rf "$ASSETS"
  mkdir -p "$ASSETS/AppIcon.appiconset"
  cp "$ICONSET"/*.png "$ICONSET"/Contents.json "$ASSETS/AppIcon.appiconset/"
  echo '{"info":{"author":"xcode","version":1}}' > "$ASSETS/Contents.json"
  xcrun actool "$ASSETS" --compile "$APP" --platform iphoneos \
    --minimum-deployment-target "$MIN_IOS" --app-icon AppIcon \
    --output-partial-info-plist "$OUT/assetcatalog.plist" >/dev/null 2>&1 \
    || echo "  (actool 未产出 Assets.car，回退平铺图标)"

  # 平铺：iPhone 60pt（1x/2x/3x）+ iPad 76pt / 83.5pt + 市场图
  cp "$ICONSET/icon-60.png"   "$APP/AppIcon60.png"
  cp "$ICONSET/icon-120.png"  "$APP/AppIcon60@2x.png"
  cp "$ICONSET/icon-180.png"  "$APP/AppIcon60@3x.png"
  cp "$ICONSET/icon-76.png"   "$APP/AppIcon76.png"
  cp "$ICONSET/icon-152.png"  "$APP/AppIcon76@2x.png"
  cp "$ICONSET/icon-167.png"  "$APP/AppIcon83.5@2x.png"
  cp "$ICONSET/icon-1024.png" "$APP/AppIcon1024.png"

  PB=/usr/libexec/PlistBuddy
  # 首选：告诉系统「图标在 Assets.car 里叫 AppIcon」。
  # 只有 actool 真的产出了 Assets.car 才写，否则会变成没有图标的空白 App。
  if [ -f "$APP/Assets.car" ]; then
    $PB -c "Delete :CFBundleIconName" "$APP/Info.plist" >/dev/null 2>&1 || true
    $PB -c "Add :CFBundleIconName string AppIcon" "$APP/Info.plist"
  fi
  # 兜底：只列**基础名**，让系统自己拼 @2x / @3x。
  $PB -c "Delete :CFBundleIconFiles" "$APP/Info.plist" >/dev/null 2>&1 || true
  $PB -c "Add :CFBundleIconFiles array" "$APP/Info.plist" >/dev/null 2>&1 || true
  for f in AppIcon60 AppIcon76 AppIcon1024; do
    $PB -c "Add :CFBundleIconFiles: string $f" "$APP/Info.plist" >/dev/null 2>&1 || true
  done
  # iPad（UIDeviceFamily 含 2）走 CFBundleIcons，缺了会在 iPad 上白图标
  $PB -c "Delete :CFBundleIcons" "$APP/Info.plist" >/dev/null 2>&1 || true
  $PB -c "Add :CFBundleIcons dict" "$APP/Info.plist" >/dev/null 2>&1 || true
  $PB -c "Add :CFBundleIcons:CFBundlePrimaryIcon dict" "$APP/Info.plist" >/dev/null 2>&1 || true
  $PB -c "Add :CFBundleIcons:CFBundlePrimaryIcon:CFBundleIconFiles array" "$APP/Info.plist" >/dev/null 2>&1 || true
  for f in AppIcon60 AppIcon76 AppIcon1024; do
    $PB -c "Add :CFBundleIcons:CFBundlePrimaryIcon:CFBundleIconFiles: string $f" "$APP/Info.plist" >/dev/null 2>&1 || true
  done
  if [ -f "$APP/Assets.car" ]; then
    $PB -c "Add :CFBundleIcons:CFBundlePrimaryIcon:CFBundleIconName string AppIcon" "$APP/Info.plist" >/dev/null 2>&1 || true
  fi
else
  echo "  跳过：缺少 assets/AppIcon.appiconset/icon-1024.png（先跑 python3 tools/gen_icon.py）"
fi

echo "== 5/6 签名（ad-hoc）=="
# 自签流程：这里先做 ad-hoc 签名让 .app 结构合法；真正装入设备时由
# SideStore / AltStore / Sideloadly 用你的开发者证书重签。
if command -v ldid >/dev/null 2>&1; then
  cat > "$OUT/entitlements.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>application-identifier</key><string>co.repo.points</string>
  <key>com.apple.developer.team-identifier</key><string>LOCAL</string>
  <key>get-task-allow</key><true/>
</dict></plist>
PLIST
  ldid -S"$OUT/entitlements.plist" "$APP/Points"
else
  codesign --force --sign - --timestamp=none "$APP" || true
fi

echo "== 6/6 打包 .ipa =="
mkdir -p "$OUT/Payload"
cp -R "$APP" "$OUT/Payload/"
( cd "$OUT" && rm -f Points.ipa && zip -qry Points.ipa Payload )

echo
echo "完成:"
echo "  APP: $APP"
echo "  IPA: $OUT/Points.ipa"
ls -lh "$OUT/Points.ipa" 2>/dev/null || true
