#!/usr/bin/env bash
# 构建 Android APK：gomobile bind → javac → d8 → aapt2 → 打包签名。
#
# 刻意不用 Gradle：
#   - 整套 Gradle + AGP 光下载就 800MB+，CI 里慢且脆；
#   - 我们的 UI 在 WebView 里，Java 层只有 9 个文件，用 build-tools 直接编更快；
#   - 依赖只有 gomobile 产出的 Mobile.aar，没有第三方库，不需要依赖解析。
#
# gomobile bind 走 **GOPATH 模式**（GO111MODULE=off），理由见 docs/gomobile-notes.md：
# module 模式会强迫 core/go.mod 依赖 golang.org/x/mobile，而后者要求 go>=1.26，
# 会把引擎的 go 指令一起抬高。GOPATH 模式完全隔离，引擎保持 go 1.21。
#
# 环境变量：
#   ANDROID_HOME / ANDROID_SDK_ROOT
#   BUILD_TOOLS  默认取 $ANDROID_HOME/build-tools 里最新的
#   PLATFORM     默认取 $ANDROID_HOME/platforms 里最新的 android-XX
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CORE="$ROOT/core"
SRC="$ROOT/android/app/src/main"
OUT="$ROOT/android/build"
GP="$(go env GOPATH)"

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
[ -d "$SDK" ] || { echo "未找到 Android SDK（ANDROID_HOME）"; exit 1; }

BT="${BUILD_TOOLS:-$(ls -1 "$SDK/build-tools" | sort -V | tail -1)}"
PLATFORM="${PLATFORM:-$(ls -1 "$SDK/platforms" | grep '^android-' | sort -V | tail -1)}"
BT_DIR="$SDK/build-tools/$BT"
PLATFORM_JAR="$SDK/platforms/$PLATFORM/android.jar"
MIN_SDK=26
TARGET_SDK=34

echo "build-tools: $BT   platform: $PLATFORM   gopath: $GP"

rm -rf "$OUT"
mkdir -p "$OUT/classes" "$OUT/dex" "$OUT/res"

echo "== 1/8 引擎单元测试 =="
( cd "$CORE" && go vet ./... && go test ./... )

echo "== 2/8 生成 Mobile.aar（GOPATH 模式）=="
# 把仓库软链进 GOPATH/src，让 gomobile 在非 module 模式下找到包
mkdir -p "$GP/src/github.com/ElyYinLi/aipoints-mobile"
ln -sfn "$CORE" "$GP/src/github.com/ElyYinLi/aipoints-mobile/core"
if [ ! -d "$GP/src/golang.org/x/mobile/bind" ]; then
  echo "缺少 \$GOPATH/src/golang.org/x/mobile（gomobile 的 bind 运行时）"
  echo "拉取方式见 docs/gomobile-notes.md"
  exit 1
fi
( cd "$GP/src/github.com/ElyYinLi/aipoints-mobile/core" && \
  GO111MODULE=off gomobile bind -target=android -androidapi "$MIN_SDK" \
    -o "$OUT/Mobile.aar" ./mobile )

echo "== 3/8 解开 Mobile.aar =="
mkdir -p "$OUT/aar"
# 先清空：gomobile 多次构建会留下上一次的 classes.jar，残留 class 会被一起打进 dex
rm -rf "$OUT/aar"/* "$OUT/classes" "$OUT/dex"
mkdir -p "$OUT/aar" "$OUT/classes" "$OUT/dex"
( cd "$OUT/aar" && unzip -qo "$OUT/Mobile.aar" )

echo "== 4/8 编译 Java =="
# 不用 -bootclasspath（JDK 9+ 下与 -target 11 冲突），直接把 android.jar 放
# classpath 最前面：javac 会优先从它解析 android.* 符号。
javac -source 11 -target 11 -nowarn \
  -classpath "$PLATFORM_JAR:$OUT/aar/classes.jar" \
  -d "$OUT/classes" \
  $(find "$SRC/java" -name '*.java')

echo "== 5/8 dex =="
"$BT_DIR/d8" --min-api "$MIN_SDK" --release \
  --lib "$PLATFORM_JAR" \
  --output "$OUT/dex" \
  $(find "$OUT/classes" -name '*.class') \
  "$OUT/aar/classes.jar"

echo "== 6/8 资源 =="
"$BT_DIR/aapt2" compile --dir "$SRC/res" -o "$OUT/res/compiled.zip"
"$BT_DIR/aapt2" link \
  -o "$OUT/base.apk" \
  -I "$PLATFORM_JAR" \
  --manifest "$SRC/AndroidManifest.xml" \
  --min-sdk-version "$MIN_SDK" \
  --target-sdk-version "$TARGET_SDK" \
  "$OUT/res/compiled.zip" \
  --auto-add-overlay

echo "== 7/8 塞入 dex 与原生库 =="
cp "$OUT/dex/classes.dex" "$OUT/"
( cd "$OUT" && zip -q base.apk classes.dex )
if [ -d "$OUT/aar/jni" ]; then
  # aapt2 产出的 APK 里没有 lib/，aar 里是 jni/<abi>/*.so，改名成 lib/<abi>/*.so
  mkdir -p "$OUT/pack"
  for abi in arm64-v8a armeabi-v7a x86_64; do
    if [ -f "$OUT/aar/jni/$abi/libgojni.so" ]; then
      mkdir -p "$OUT/pack/lib/$abi"
      cp "$OUT/aar/jni/$abi/libgojni.so" "$OUT/pack/lib/$abi/"
    fi
  done
  ( cd "$OUT/pack" && zip -qr "$OUT/base.apk" lib )
fi

echo "== 8/8 对齐 + 签名 =="
"$BT_DIR/zipalign" -f -p 4 "$OUT/base.apk" "$OUT/aligned.apk"

# 签名密钥必须跨构建固定，否则每次产出的 APK 签名不同：
# 用户覆盖安装会失败，只能先卸载（本地账号与登录态全丢）。
# 因此把密钥放在源码目录 android/signing/ 下随仓库走，而不是 build/ 里现生成。
# 这是自签名自用场景；密钥泄露只影响「别人能否伪造升级包」，不涉及账号安全。
KS="${POINTS_KEYSTORE:-$ROOT/android/signing/points.jks}"
if [ ! -f "$KS" ]; then
  echo "缺少签名密钥：$KS"
  echo "首次构建可执行：keytool -genkeypair -v -keystore \"$KS\" -storepass points2024 \"
  echo "  -keypass points2024 -alias points -keyalg RSA -keysize 2048 -validity 10000 \"
  echo "  -dname CN=Points,OU=Self,O=Self,L=Local,C=CN"
  exit 1
fi
"$BT_DIR/apksigner" sign \
  --ks "$KS" --ks-pass pass:points2024 --key-pass pass:points2024 \
  --ks-key-alias points \
  --out "$OUT/Points.apk" "$OUT/aligned.apk"

"$BT_DIR/apksigner" verify --print-certs "$OUT/Points.apk" | head -3
echo
echo "完成: $OUT/Points.apk"
du -h "$OUT/Points.apk"
