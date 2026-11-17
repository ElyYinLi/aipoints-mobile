#!/usr/bin/env bash
# 构建 iOS 静态库（Mobile.xcframework）与可自签的 .app / .ipa。
#
# 只在 macOS 上可跑。CI 里对应 .github/workflows/build-ios.yml。
#
# 设计取舍（与常见方案对比）：
#   1. 不用 xcodeproj —— 本仓库没有 .xcodeproj，直接用 swiftc 手工组装 .app。
#      好处：免去 `xcodebuild -project` 对工程文件的依赖，CI 上不需要生成工程；
#      代价：资源要用 actool 单独编，或用现成的 Info.plist + Assets。
#   2. gomobile bind 产出的是【静态库】而非 dylib —— 后者会被塞进 Frameworks/
#      目录，自签工具（LiveContainer 等）会报 `Invalid mach-o file!`。
#   3. 只编 arm64（真机）+ 可选模拟器切片，减小产物。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CORE="$ROOT/core"
OUT="$ROOT/ios/build"
APP="$OUT/Points.app"
SDK_IPHONEOS="$(xcrun --sdk iphoneos --show-sdk-path)"
MIN_IOS="15.0"

mkdir -p "$OUT" "$APP"

echo "== 1/6 生成 Mobile.xcframework =="
if [ ! -d "$OUT/Mobile.xcframework" ]; then
  ( cd "$CORE" && go build ./... )
  command -v gomobile >/dev/null || {
    go install golang.org/x/mobile/cmd/gomobile@latest
    go install golang.org/x/mobile/cmd/gobind@latest
  }
  ( cd "$CORE" && gomobile bind -target=ios -iosversion="$MIN_IOS" \
      -o "$OUT/Mobile.xcframework" ./mobile )
fi

echo "== 2/6 编译 Swift =="
SWIFT_SOURCES=$(find "$ROOT/ios/App/Sources" -name '*.swift')
# shellcheck disable=SC2086
xcrun -sdk iphoneos swiftc \
  -target arm64-apple-ios${MIN_IOS} \
  -sdk "$SDK_IPHONEOS" \
  -parse-as-library \
  -O \
  -F "$OUT/Mobile.xcframework/ios-arm64" \
  -framework Mobile \
  -framework SwiftUI -framework WebKit -framework Security \
  -Xlinker -rpath -Xlinker /usr/lib/swift \
  -Xlinker -e -Xlinker _main \
  $SWIFT_SOURCES \
  -o "$APP/Points"

echo "== 3/6 拷静态库与 Swift 运行时 =="
cp "$OUT/Mobile.xcframework/ios-arm64/Mobile.framework/Mobile" "$OUT/libMobile.a" 2>/dev/null || true
# gomobile 的静态切片在 ios-arm64/ 下，直接并入主二进制（静态链接已在上一步完成）

SWIFT_LIB="$APP/Frameworks"
mkdir -p "$SWIFT_LIB"
if [ -d "/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/lib/swift/iphoneos" ]; then
  for f in libswiftCore.dylib libswift_Concurrency.dylib libswift_StringProcessing.dylib; do
    src="/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/lib/swift/iphoneos/$f"
    [ -f "$src" ] && cp "$src" "$SWIFT_LIB/" || true
  done
fi

echo "== 4/6 安装 Info.plist 与图标 =="
cp "$ROOT/ios/App/Info.plist" "$APP/Info.plist"
if [ -d "$ROOT/ios/App/Assets.xcassets" ]; then
  xcrun actool "$ROOT/ios/App/Assets.xcassets" \
    --compile "$APP" \
    --platform iphoneos \
    --minimum-deployment-target "$MIN_IOS" \
    --app-icon AppIcon \
    --output-partial-info-plist "$OUT/assetcatalog.plist" >/dev/null || true
fi

echo "== 5/6 签名（ad-hoc，仅用于本机自签）=="
# 自签流程：这里先做 ad-hoc 签名让 .app 结构合法；真正装入设备时由
# SideStore / AltStore / Sideloadly 用你的开发者证书重签。
codesign --force --sign - --timestamp=none "$APP" || true

echo "== 6/6 打包 .ipa =="
rm -rf "$OUT/Payload" && mkdir -p "$OUT/Payload"
cp -R "$APP" "$OUT/Payload/"
( cd "$OUT" && rm -f Points.ipa && zip -qry Points.ipa Payload )

echo
echo "完成:"
echo "  APP: $APP"
echo "  IPA: $OUT/Points.ipa"
du -sh "$OUT/Points.ipa" 2>/dev/null || true
