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
