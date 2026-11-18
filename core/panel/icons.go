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
		// 汇总：四格 + 星
		"spark": `<path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z"/>
		          <path d="M18.5 15.5l.8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2-2.2-.8 2.2-.8z"/>`,
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
		// 主题三态
		"sun":  `<circle cx="12" cy="12" r="4"/><path d="M12 2.6v2.2M12 19.2v2.2M21.4 12h-2.2M4.8 12H2.6M18.6 5.4l-1.5 1.5M6.9 17.1l-1.5 1.5M18.6 18.6l-1.5-1.5M6.9 6.9L5.4 5.4"/>`,
		"moon": `<path d="M20.5 14.3A8.6 8.6 0 0 1 9.7 3.5a8.6 8.6 0 1 0 10.8 10.8z"/>`,
		"auto": `<circle cx="12" cy="12" r="8.5"/><path d="M12 3.5v17"/>`,
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
	}
	path, ok := body[name]
	if !ok {
		path = body["spark"]
	}
	return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
	  stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + path + `</svg>`
}

// appIcon 生成 App 图标（1024×1024，矢量，可直接导出 PNG）。
// 设计：深蓝紫渐变底 + 白色「积分堆叠」符号 + 右上角签到勾。
const appIcon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
  <defs>
    <linearGradient id="g" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#4F6BFF"/>
      <stop offset="0.55" stop-color="#7B4DFF"/>
      <stop offset="1" stop-color="#C24DFF"/>
    </linearGradient>
    <linearGradient id="s" x1="0.5" y1="0" x2="0.5" y2="1">
      <stop offset="0" stop-color="#fff" stop-opacity="0.55"/>
      <stop offset="1" stop-color="#fff" stop-opacity="0.32"/>
    </linearGradient>
  </defs>
  <rect width="1024" height="1024" rx="232" fill="url(#g)"/>
  <!-- 三层积分卡：自下而上收窄，读起来是「余额逐层累积」 -->
  <rect x="292" y="676" width="440" height="118" rx="59" fill="url(#s)"/>
  <rect x="264" y="512" width="496" height="118" rx="59" fill="url(#s)" opacity="0.78"/>
  <rect x="236" y="348" width="552" height="118" rx="59" fill="#fff" opacity="0.96"/>
  <!-- 签到勾：独立于卡片，落在右上角形成视觉重量 -->
  <path d="M596 246l52 52 104-118" fill="none" stroke="#22D48A" stroke-width="58"
        stroke-linecap="round" stroke-linejoin="round"/>
</svg>`
