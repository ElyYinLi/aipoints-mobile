package panel

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 0.6.8：App 图标改为主界面左上角同款 spark 双星。
// 三端（面板 appIcon 常量 / Android VectorDrawable / SVG 源）必须用同一组星形坐标，
// 否则又会出现「桌面图标和界面图标对不上」——正是这次要修的问题。
func TestAppIconUsesSparkDoubleStar(t *testing.T) {
	// 面板内 appIcon 常量：白色双星、无旧的积分卡/签到勾
	for _, want := range []string{
		"stroke=\"#fff\"",
		"M474.5,234.5", // 主星起点
		"M669.5,609.5", // 副星起点
	} {
		if !strings.Contains(appIcon, want) {
			t.Fatalf("appIcon 缺少 spark 双星要素：%q", want)
		}
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
	for _, coord := range []string{"M474.5,234.5", "M669.5,609.5"} {
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
	for _, coord := range []string{"M474.5,234.5", "M669.5,609.5"} {
		if !strings.Contains(svg, coord) {
			t.Fatalf("SVG 图标缺少与 appIcon 一致的星形坐标 %q", coord)
		}
	}
}
