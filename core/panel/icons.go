// Package panel — 面板前端资源。
//
// 设计约束：
//   - 单文件、零外部依赖（无 CDN、无字体文件），离线可用；
//   - 所有图标为内联 SVG，随 HTML 一起走，不额外发请求；
//   - 交互动效全部走 transform/opacity（合成层），不触发重排，避免卡顿；
//   - 列表用 DocumentFragment 批量插入，避免逐条 append 引起多次重排。
package panel

// iconSVG 是自有图标系统：24×24 网格、圆角描边、统一 1.8 线宽。
// 返回可直接插入 innerHTML 的 <svg> 片段。
func iconSVG(name string) string {
	body := map[string]string{
		// 汇总：V4 双星（0.7.0 起与 App 图标同源）。
		//
		// 这里以前是两枚 **实心** 五角星（1.9/0.8 单位），和 App 图标的
		// **空心描边**双星是完全独立的两套图形——改 App 图标时左上角这个
		// logo 永远不会跟着变，用户看到的就是「进了 App 图标还是旧的」。
		//
		// 现在按 App 图标的 1024 画布坐标等比映射到 24 网格（除以 42.67），
		// 描边宽度同步缩放：主星 48/42.67≈1.13，副星 28/42.67≈0.66。
		// 图形形状、两个星的相对位置与 App 图标完全一致。
		"spark": `<path d="M10.66,4.57 L12.05,8.34 L15.82,9.73 L12.05,11.11 L10.66,14.88 L9.27,11.11 L5.5,9.73 L9.27,8.34 Z"
		            stroke-width="1.13"/>
		          <path d="M16.64,14.60 L17.34,16.52 L19.26,17.22 L17.34,17.93 L16.64,19.85 L15.93,17.93 L14.01,17.22 L15.93,16.52 Z"
		            stroke-width="0.66"/>`,
		"refresh":  `<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>`,
		"check":    `<path d="M4.5 12.5l5 5 10-11"/>`,
		"checkAll": `<path d="M1.5 12.5l4.5 4.5 8-9"/><path d="M8.5 16l1.5 1.5 11-12"/>`,
		"plus":     `<path d="M12 5v14"/><path d="M5 12h14"/>`,
		// 齿轮：外圈八齿 + 中心圆孔。
		// 之前是「小圆 + 八条放射短线」，和太阳图标几乎一模一样，
		// 放到 40px 的按钮里根本分不出来（用户直接反馈「小太阳是设置？」）。
		// 现在画成真正的齿廓，靠轮廓就能认出来这是齿轮。
		"gear": `<path d="M12 2.6l1.9 1.1 2.2-.3 1.2 1.9 2 1 .2 2.2 1.5 1.6-1.5 1.6-.2 2.2-2 1-1.2 1.9-2.2-.3L12 21.4l-1.9-1.1-2.2.3-1.2-1.9-2-1-.2-2.2L3 12l1.5-1.6.2-2.2 2-1 1.2-1.9 2.2.3z"/>
		           <circle cx="12" cy="12" r="3.1"/>`,
		"close": `<path d="M6 6l12 12M18 6L6 18"/>`,
		"arrow": `<path d="M5 12h13"/><path d="M12.5 5.5L19 12l-6.5 6.5"/>`,
		"link": `<path d="M10.5 13.5a3.5 3.5 0 0 0 5 0l3-3a3.5 3.5 0 0 0-5-5l-1.5 1.5"/>
		            <path d="M13.5 10.5a3.5 3.5 0 0 0-5 0l-3 3a3.5 3.5 0 0 0 5 5l1.5-1.5"/>`,
		"warn":  `<path d="M12 4l9 16H3z"/><path d="M12 10v4.5"/><circle cx="12" cy="17.4" r=".9" fill="currentColor" stroke="none"/>`,
		"clock": `<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>`,
		"coin":  `<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5v9M9.5 9.8h3.8a1.9 1.9 0 0 1 0 3.8H9.5"/>`,
		// 任务清单 + 对勾
		"task": `<rect x="4" y="3.5" width="15" height="15" rx="3"/><path d="M8.5 12l2.5 2.5 5-5.5"/>`,
		// 主题两态（跟随系统已移除）
		"sun":  `<circle cx="12" cy="12" r="4"/><path d="M12 2.6v2.2M12 19.2v2.2M21.4 12h-2.2M4.8 12H2.6M18.6 5.4l-1.5 1.5M6.9 17.1l-1.5 1.5M18.6 18.6l-1.5-1.5M6.9 6.9L5.4 5.4"/>`,
		"moon": `<path d="M20.5 14.3A8.6 8.6 0 0 1 9.7 3.5a8.6 8.6 0 1 0 10.8 10.8z"/>`,
		// 说明：圆圈 + 小写 i
		"info": `<circle cx="12" cy="12" r="8.6"/><path d="M12 10.6v6"/><circle cx="12" cy="7.6" r="1" fill="currentColor" stroke="none"/>`,
		// 视图密度：list = 三行条目 / grid = 四格平铺
		"list": `<path d="M8 6.5h12M8 12h12M8 17.5h12"/>
		         <circle cx="4" cy="6.5" r="1.1" fill="currentColor" stroke="none"/>
		         <circle cx="4" cy="12" r="1.1" fill="currentColor" stroke="none"/>
		         <circle cx="4" cy="17.5" r="1.1" fill="currentColor" stroke="none"/>`,
		"grid": `<rect x="3.5" y="3.5" width="7" height="7" rx="2"/><rect x="13.5" y="3.5" width="7" height="7" rx="2"/>
		         <rect x="3.5" y="13.5" width="7" height="7" rx="2"/><rect x="13.5" y="13.5" width="7" height="7" rx="2"/>`,
		// 抽奖：票根（虚线是撕口）
		"ticket": `<path d="M4 8.5a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v1.6a2 2 0 0 0 0 3.8v1.6a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-1.6a2 2 0 0 0 0-3.8z"/>
		           <path d="M13.5 7.5v9" stroke-dasharray="2 2.4"/>`,
		// 奖品：奖杯
		"trophy": `<path d="M8 4.5h8v4a4 4 0 0 1-8 0z"/><path d="M8 5.5H5.5v1.5A3 3 0 0 0 8 10"/>
		           <path d="M16 5.5h2.5v1.5A3 3 0 0 1 16 10"/><path d="M12 12.5v3.5"/><path d="M8.5 19.5h7"/>`,
		// API 令牌：钥匙（齿朝下，环在左上，与「盾牌/锁」区分）
		"key": `<circle cx="8.5" cy="8.5" r="3.6"/><path d="M11.1 11.1L19 19"/><path d="M16.2 16.2l2-2"/>`,
		// 通知：铃铛
		"bell": `<path d="M6.5 10a5.5 5.5 0 0 1 11 0c0 4 1.4 5.4 1.9 6.2.2.3 0 .8-.4.8H5c-.4 0-.6-.5-.4-.8.5-.8 1.9-2.2 1.9-6.2z"/>
		         <path d="M10 20.2a2.2 2.2 0 0 0 4 0"/>`,
	}
	path, ok := body[name]
	if !ok {
		path = body["spark"]
	}
	// spark 自己带 stroke-width（主星 1.13 / 副星 0.66，取自 App 图标的等比映射），
	// 外层 <svg> 若再写死 stroke-width="1.8"，两星会一样粗——那正是 0.6.9 之前
	// 左上角 logo 和 App 图标对不上的原因之一。
	if name == "spark" {
		return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"
	  stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + path + `</svg>`
	}
	return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
	  stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + path + `</svg>`
}

// appIcon 生成 App 图标（1024×1024，矢量，可直接导出 PNG）。
// 设计（0.6.9）：深蓝紫渐变底 + 白色 spark 双星，与面板左上角 logo 同源。
// 用户反馈 0.6.8 版小星描边过粗（46 相对自身半径 90 占比 0.51），空心被挤成一点、桌上看着糊。
// 0.6.9 采用「V4 错位呼应」：大星居中偏上、小星放大并下移到右下、两星拉开距离，
// 关键是小星描边按尺寸等比减细到 28 —— 空心清晰可见，小星绝不加粗（用户明确要求）。
//
// 这两条 path 由 spark 的 24×24 网格坐标映射到 1024 画布得到，
// 三端（assets/*.svg / *.png / Android ic_launcher.xml）共用同一组坐标，不要单独改。
// 0.7.0 规范修正：去掉 rx 圆角，改满幅方形。
// iOS 自己按 ≈22.37% 裁圆角；图标预烘焙圆角后，圆角外侧的透明像素在部分
// 渲染路径（第三方商店 / 重签安装）会被填成纯黑，边缘发脏。
// 图形坐标与描边宽度一个字没动 —— 用户明确拒绝改描边。
const appIcon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#4F6BFF"/>
      <stop offset="0.55" stop-color="#7B4DFF"/>
      <stop offset="1" stop-color="#C24DFF"/>
    </linearGradient>
  </defs>
  <rect width="1024" height="1024" fill="url(#g)"/>
  <!-- 0.6.9 V4：主界面左上角同款 spark 双星，白色空心描边，错位呼应 -->
  <!-- 主星：居中偏上 -->
  <path d="M455,195 L514.1,355.9 L675,415 L514.1,474.1 L455,635 L395.9,474.1 L235,415 L395.9,355.9 Z"
        fill="none" stroke="#fff" stroke-width="48" stroke-linecap="round" stroke-linejoin="round"/>
  <!-- 副星：右下角，放大到 112 半径、描边减细到 28，空心清晰 -->
  <path d="M710,623 L740.1,704.9 L822,735 L740.1,765.1 L710,847 L679.9,765.1 L598,735 L679.9,704.9 Z"
        fill="none" stroke="#fff" stroke-width="28" stroke-linecap="round" stroke-linejoin="round"/>
</svg>`
