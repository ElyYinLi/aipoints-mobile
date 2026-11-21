package panel

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 0.6.9：App 图标改为 V4「错位呼应」spark 双星（小星放大、描边减细、空心清晰）。
// 三端（面板 appIcon 常量 / Android VectorDrawable / SVG 源）必须用同一组星形坐标，
// 否则又会出现「桌面图标和界面图标对不上」——正是这次要修的问题。
func TestAppIconUsesSparkDoubleStar(t *testing.T) {
	// 面板内 appIcon 常量：白色双星、无旧的积分卡/签到勾
	for _, want := range []string{
		"stroke=\"#fff\"",
		"M455,195", // 主星起点（V4 错位呼应）
		"M710,623", // 副星起点
	} {
		if !strings.Contains(appIcon, want) {
			t.Fatalf("appIcon 缺少 spark 双星要素：%q", want)
		}
	}
	// 小星描边必须减细到 28（0.6.8 的 46 把空心挤成一点、桌上看着糊）。绝不能再加粗。
	if !strings.Contains(appIcon, `stroke-width="28"`) {
		t.Fatal("小星描边未减细到 28，空心会again被挤成一点")
	}
	for _, dead := range []string{"#22D48A", "积分卡", "签到勾", "url(#s)"} {
		if strings.Contains(appIcon, dead) {
			t.Fatalf("appIcon 仍残留旧图标元素：%q", dead)
		}
	}

	// 左上角 logo 用的就是 spark，且 appIcon 注释要点明同源
	if !strings.Contains(iconSVG("spark"), "<path") {
		t.Fatal("spark 图标应存在")
	}
	if !strings.Contains(appIcon, "spark 双星") {
		t.Fatal("appIcon 未标注与 spark 同源")
	}
}

// 0.7.0：图标规范修正。图形一律不动（用户明确拒绝改描边），
// 但必须满足 Apple 的硬规范，否则真机上会出现黑边 / 边缘发脏。
func TestAppIconMeetsAppleSpec(t *testing.T) {
	// 1. 不能预烘焙圆角：rx 会让圆角外侧的透明像素在部分渲染路径上变纯黑
	if strings.Contains(appIcon, "rx=") {
		t.Fatal("appIcon 仍带圆角（rx=），iOS 会自己裁，预烘焙会导致黑边")
	}
	// 2. 满幅方形底：从 0,0 铺满 1024
	if !strings.Contains(appIcon, `<rect width="1024" height="1024" fill="url(#g)"/>`) {
		t.Fatal("appIcon 底色不是满幅方形，四角会露出透明")
	}
	// 3. 描边宽度保持不变（用户红线：不允许改细）
	if !strings.Contains(appIcon, `stroke-width="48"`) {
		t.Fatal("主星描边宽度被改动，用户明确要求保持 48")
	}
	if !strings.Contains(appIcon, `stroke-width="28"`) {
		t.Fatal("副星描边宽度被改动，用户明确要求保持 28")
	}
}

// 左上角 logo 必须与 App 图标「同源」：空心双星 + 同比例的粗细。
// 0.6.9 之前这里是两枚实心星，改 App 图标时 logo 永远不跟着变——
// 用户实测反馈「进了 App 图标还是旧的」，根因就是这个。
func TestBrandLogoMatchesAppIcon(t *testing.T) {
	logo := iconSVG("spark")
	// 空心：不能有 fill="currentColor" 这类实心填充
	if strings.Contains(logo, `fill="currentColor"`) {
		t.Fatal("左上角 logo 仍是实心星，应与 App 图标一样是空心描边")
	}
	// 双星：两条 path
	if got := strings.Count(logo, "<path"); got != 2 {
		t.Fatalf("左上角 logo 应为双星（2 条 path），实际 %d 条", got)
	}
	// 线宽取自 App 图标的等比映射：48/42.67≈1.13、28/42.67≈0.66
	for _, w := range []string{`stroke-width="1.13"`, `stroke-width="0.66"`} {
		if !strings.Contains(logo, w) {
			t.Fatalf("左上角 logo 线宽 %q 与 App 图标比例不一致", w)
		}
	}
	// 小星必须比主星细（用户：小星不许加粗）
	if strings.Contains(logo, `stroke-width="1.8"`) {
		t.Fatal("左上角 logo 还在用默认 1.8 线宽，双星粗细会一样")
	}
}

// iOS 全尺寸图标产物必须合规：13 个尺寸、无 alpha、四角不透明。
// 以前只有 4 张平铺 PNG 且靠 sips 现缩，主屏/设置页/App Store 各糊一遍。
func TestIOSAppIconSetComplete(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	dir := filepath.Join(repoRoot, "assets", "AppIcon.appiconset")
	cj := filepath.Join(dir, "Contents.json")
	b, err := os.ReadFile(cj)
	if err != nil {
		t.Fatalf("读取 %s 失败：%v（先跑 python3 tools/gen_icon.py）", cj, err)
	}
	// 18 条目录项（13 个去重尺寸）
	if got := strings.Count(string(b), `"filename"`); got != 18 {
		t.Fatalf("Contents.json 应有 18 条目录项，实际 %d", got)
	}
	// 关键尺寸必须都有实体文件
	for _, px := range []int{40, 58, 60, 80, 87, 120, 152, 167, 180, 1024} {
		p := filepath.Join(dir, "icon-"+itoa(px)+".png")
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("缺少图标尺寸 %dpx：%v", px, err)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Android ic_launcher.xml 必须与 appIcon 用同一组星形坐标。
func TestAndroidIconMatchesAppIcon(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	p := filepath.Join(repoRoot, "android", "app", "src", "main", "res", "drawable", "ic_launcher.xml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 ic_launcher.xml 失败：%v", err)
	}
	xml := string(b)
	for _, coord := range []string{"M455,195", "M710,623"} {
		if !strings.Contains(xml, coord) {
			t.Fatalf("Android 图标缺少与 appIcon 一致的星形坐标 %q", coord)
		}
	}
	// 旧签到勾的绿色必须消失
	if strings.Contains(xml, "#FF22D48A") {
		t.Fatal("Android 图标仍是旧的绿色签到勾")
	}
}

// SVG 源文件同样要同步为 spark 双星（iOS PNG 由它渲染）。
func TestSvgIconMatchesAppIcon(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	p := filepath.Join(repoRoot, "assets", "app-icon.svg")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 app-icon.svg 失败：%v", err)
	}
	svg := string(b)
	for _, coord := range []string{"M455,195", "M710,623"} {
		if !strings.Contains(svg, coord) {
			t.Fatalf("SVG 图标缺少与 appIcon 一致的星形坐标 %q", coord)
		}
	}
}
