package panel

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 图标「不清晰」的真因是**声明的文件名不对**，不是画得不够锐。
//
// 现场：0.7.0/0.7.1 的 IPA 里
//
//	CFBundleIconFiles = ["AppIcon60","AppIcon120","AppIcon180"]
//	.app 根目录        = AppIcon60.png(60) AppIcon120.png(120) AppIcon180.png(180)
//
// 而 iOS 解析 CFBundleIconFiles 的规则是「基础名 + @2x/@3x」：
// 对 AppIcon60 只找 AppIcon60.png / AppIcon60@2x.png / AppIcon60@3x.png。
// AppIcon120、AppIcon180 是**另外两个基础名**，匹配 AppIcon60 时根本不会被看到。
// @3x 屏要 180px，系统只找到 60px 的 AppIcon60.png → 放大 3 倍 → 必然糊。
//
// 修法：CFBundleIconName=AppIcon 走 Assets.car（13 尺寸全套），
// 平铺文件按 name@2x/@3x 正确命名兜底。
func iconScript(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	p := filepath.Join(repoRoot, "ios", "build-ipa.sh")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 build-ipa.sh 失败：%v", err)
	}
	return string(b)
}

func TestIconDeclaredFromAssetCatalog(t *testing.T) {
	sh := iconScript(t)
	if !strings.Contains(sh, "CFBundleIconName string AppIcon") {
		t.Fatal("缺少 CFBundleIconName=AppIcon：不写它，iOS 不会读 Assets.car 里的全套尺寸")
	}
	// 只有真的产出了 Assets.car 才写，否则会变成空白图标
	if !strings.Contains(sh, `if [ -f "$APP/Assets.car" ]`) {
		t.Fatal("CFBundleIconName 未加 Assets.car 存在性判断")
	}
}

func TestIconFlatFilesUseScaleSuffix(t *testing.T) {
	sh := iconScript(t)

	// 这三个名字是 iOS 唯一认的 iPhone 主屏图标
	for _, name := range []string{"AppIcon60.png", "AppIcon60@2x.png", "AppIcon60@3x.png"} {
		if !strings.Contains(sh, `"$APP/`+name+`"`) {
			t.Fatalf("缺少平铺图标 %s：iOS 只会按 基础名+@2x/@3x 查找", name)
		}
	}
	// iPad
	for _, name := range []string{"AppIcon76.png", "AppIcon76@2x.png", "AppIcon83.5@2x.png"} {
		if !strings.Contains(sh, `"$APP/`+name+`"`) {
			t.Fatalf("缺少 iPad 平铺图标 %s", name)
		}
	}
}

func TestIconFilesListUsesBaseNamesOnly(t *testing.T) {
	sh := iconScript(t)

	// CFBundleIconFiles 必须只列基础名，系统自己拼 @2x/@3x。
	// 列成 AppIcon120 / AppIcon180 等于声明了两个不存在尺寸的基础名。
	idx := strings.Index(sh, `Add :CFBundleIconFiles array`)
	if idx < 0 {
		t.Fatal("缺少 CFBundleIconFiles")
	}
	tail := sh[idx:]
	if end := strings.Index(tail, `Add :CFBundleIcons dict`); end > 0 {
		tail = tail[:end]
	}
	for _, bad := range []string{"AppIcon120", "AppIcon180"} {
		if strings.Contains(tail, bad) {
			t.Fatalf("CFBundleIconFiles 里仍有 %s：它不是基础名，系统不会拿它当 180px 图标", bad)
		}
	}
	for _, good := range []string{"AppIcon60", "AppIcon76", "AppIcon1024"} {
		if !strings.Contains(tail, good) {
			t.Fatalf("CFBundleIconFiles 缺少基础名 %s", good)
		}
	}
}

// 图标源本身仍要符合 Apple 规范（这些断言在 icons_test.go 里，此处只补版本一致性）。
func TestVersionConsistentAcrossPlatforms(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", rel, err)
		}
		return string(b)
	}

	if !strings.Contains(read("core/mobile/mobile.go"), `var Version = "0.7.2"`) {
		t.Error("core/mobile/mobile.go 版本不是 0.7.2")
	}
	plist := read("ios/App/Info.plist")
	if !strings.Contains(plist, "<string>0.7.2</string>") || !strings.Contains(plist, "<string>27</string>") {
		t.Error("iOS Info.plist 版本/build 不是 0.7.2 (27)")
	}
	man := read("android/app/src/main/AndroidManifest.xml")
	if !strings.Contains(man, `android:versionName="0.7.2"`) || !strings.Contains(man, `android:versionCode="27"`) {
		t.Error("AndroidManifest 版本/build 不是 0.7.2 (27)")
	}
}
