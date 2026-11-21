package panel

import "strings"

// indexHTML 是面板的单文件前端。
//
// 工程取舍：
//   - 不用任何框架/构建链：包体 0，离线可用，壳层 WebView 首屏无白屏等待；
//   - 视觉走 Material 3 的语汇（大圆角、柔和阴影、胶囊按钮、tonal surface）；
//   - 下拉刷新用 pointer 事件手写，配合 CSS transform 做阻尼回弹；
//   - 所有动效只用 transform/opacity，避免合成层抖动（卡顿主因）；
//   - 列表渲染用 DocumentFragment 一次插入，杜绝逐条 append 的多次重排；
//   - prefers-reduced-motion 下自动降级为无动画，尊重系统无障碍设置。
const indexTmpl = `<!DOCTYPE html>
<html lang="zh-CN"><head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover,maximum-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="theme-color" content="#141218">
<title>积分矩阵</title>
<style>
/* ---------- 设计令牌（Material 3 tonal palette） ---------- */
:root{
  --primary:#4F5BD5; --on-primary:#fff;
  --primary-c:#E2E0FF; --on-primary-c:#14146B;
  --surface:#FBF8FF; --surface-1:#F4F1FB; --surface-2:#EEEAFA; --surface-3:#E8E3F7;
  --card:#FFFFFF; --on-surface:#1B1B22; --on-surface-v:#5A5866;
  --outline:#C9C5D8; --outline-v:#E4E1EF;
  --ok:#1E7F58; --ok-c:#C8F2DF;
  --warn:#9A6A00; --warn-c:#FFE6B3;
  --bad:#B3261E; --bad-c:#FFDAD6;
  --shadow:0 1px 2px rgba(20,18,30,.06),0 4px 16px rgba(20,18,30,.05);
  --shadow-lg:0 8px 28px rgba(20,18,30,.14);
  --r-xl:28px; --r-lg:20px; --r-md:14px; --r-pill:999px;
  --mono:ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace;
  --hair:rgba(128,128,150,.16);
  --sat:env(safe-area-inset-top); --sab:env(safe-area-inset-bottom);
}
/* 深色令牌：只由 data-theme 决定。
   已经删掉 prefers-color-scheme 兜底——Android WebView 上它不可靠，
   「跟随系统」这个选项本身也已从 UI 移除。 */
/* data-theme=dark：唯一决定深色的开关 */
:root[data-theme="dark"]{
  --primary:#BAC3FF; --on-primary:#1B2270;
  --primary-c:#343D8C; --on-primary-c:#DEE0FF;
  --surface:#141218; --surface-1:#1D1B22; --surface-2:#232128; --surface-3:#2B2833;
  --card:#1F1D25; --on-surface:#E6E1E9; --on-surface-v:#A8A4B4;
  --outline:#474452; --outline-v:#332F3C;
  --ok:#7FD9B0; --ok-c:#12452F;
  --warn:#F5C25B; --warn-c:#4A3708;
  --bad:#FFB4AB; --bad-c:#5C1410;
  --shadow:0 1px 2px rgba(0,0,0,.35),0 4px 16px rgba(0,0,0,.28);
  --shadow-lg:0 10px 32px rgba(0,0,0,.55);
}
*{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
html{height:100%}
body{
  margin:0;background:var(--surface);color:var(--on-surface);
  font:15px/1.55 -apple-system,BlinkMacSystemFont,"PingFang SC","Noto Sans SC",system-ui,sans-serif;
  -webkit-font-smoothing:antialiased;
  overscroll-behavior-y:contain;   /* 禁掉 WebView 自身的橡皮筋，交给我们自己的下拉刷新 */
  /* 给固定底栏留空间：底栏高度 ≈ 按钮 43 + 上下内边距 16 + 安全区 */
  padding-bottom:calc(70px + var(--sab));
}
svg{width:22px;height:22px;display:block;flex:0 0 auto}

/* ---------- 顶部 App Bar ---------- */
.appbar{
  position:sticky;top:0;z-index:30;
  padding:calc(10px + var(--sat)) 18px 10px;
  background:color-mix(in srgb,var(--surface) 88%,transparent);
  backdrop-filter:saturate(180%) blur(18px);-webkit-backdrop-filter:saturate(180%) blur(18px);
  display:flex;align-items:center;gap:12px;
  border-bottom:1px solid transparent;
  transition:border-color .2s;
}
.appbar.scrolled{border-bottom-color:var(--outline-v)}
.brand{display:flex;align-items:center;gap:10px;flex:1;min-width:0}
.brand .logo{
  width:34px;height:34px;border-radius:11px;flex:0 0 auto;
  background:linear-gradient(135deg,#4F6BFF,#7B4DFF 55%,#C24DFF);
  display:grid;place-items:center;color:#fff;
  box-shadow:0 4px 14px rgba(111,80,255,.35);
}
.brand .logo svg{width:20px;height:20px}
.brand h1{font-size:16.5px;margin:0;font-weight:650;letter-spacing:-.01em;white-space:nowrap}
.iconbtn{
  width:40px;height:40px;border-radius:var(--r-pill);border:0;padding:0;
  display:grid;place-items:center;background:transparent;color:var(--on-surface-v);
  transition:background .16s,color .16s;
}
.iconbtn:active{background:var(--surface-3);color:var(--on-surface)}
.iconbtn.spin svg{animation:spin .8s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}

/* ---------- 活动条（签到进度） ---------- */
/* 贴在 appbar 和汇总区之间，不占常规布局高度（默认 display:none）。
   三态：进行中（主色 + 呼吸点）/ 完成（绿）/ 失败（红）。 */
.act{
  display:none;align-items:center;gap:9px;
  margin:10px 18px 0;padding:10px 14px;
  border-radius:var(--r-md);
  background:var(--primary-c);color:var(--on-primary-c);
  font-size:13px;line-height:1.35;font-weight:500;
  animation:actIn .22s ease;
}
.act.on{display:flex}
.act.done{background:var(--ok-c);color:var(--ok)}
.act.err{background:var(--bad-c);color:var(--bad)}
.act-dot{
  width:8px;height:8px;border-radius:50%;flex:0 0 auto;
  background:var(--primary);animation:pulse 1.1s ease-in-out infinite;
}
.act.done .act-dot{background:var(--ok);animation:none}
.act.err .act-dot{background:var(--bad);animation:none}
.act-txt{flex:1;min-width:0}
@keyframes pulse{0%,100%{opacity:1;transform:scale(1)}50%{opacity:.45;transform:scale(.82)}}
@keyframes actIn{from{opacity:0;transform:translateY(-4px)}to{opacity:1;transform:none}}

/* ---------- 汇总卡 ---------- */
.hero{padding:14px 18px 6px}
/* 合计区的「全部账号合计 + 大字 + 单位」三件套整体可隐藏：
   单位不一致时这个数没有意义，留着只是占掉半屏。
   统计条（chips-row）不属于合计，要始终显示，所以不放进这个容器。 */
.hero-head{padding:0}
.hero-head.off{display:none}
.hero .kicker{font-size:12.5px;color:var(--on-surface-v);letter-spacing:.02em;margin-bottom:2px}
.total{
  display:flex;align-items:baseline;gap:8px;
  font-variant-numeric:tabular-nums;
}
/* 注意：[hidden] 在 .total{display:flex} 面前无效（元素自身 display 优先），
   所以隐藏必须用这条类，而不是 hidden 属性 */
.total.off{display:none}
.total .num{
  font-size:42px;font-weight:700;letter-spacing:-.035em;line-height:1.06;
  background:linear-gradient(120deg,var(--primary),#8B4DFF 90%);
  -webkit-background-clip:text;background-clip:text;-webkit-text-fill-color:transparent;
}
/* 占位（破折号）已改为整行隐藏（见 .total.off）。
   这里保留最小化样式，避免残留的 .dash 元素被渐变裁成一根横条。 */
.total .num.dash{
  background:none!important;-webkit-text-fill-color:var(--on-surface-v)!important;
  color:var(--on-surface-v);font-size:15px;font-weight:600;
}
.total .unit{font-size:14px;color:var(--on-surface-v);font-weight:500}
.chips-row{display:flex;align-items:center;gap:8px;margin-top:12px}
.chips{display:flex;gap:8px;flex-wrap:wrap;flex:1;min-width:0}
/* 视图密度切换：放在「N 个正常 / N 个已签」右侧 */
.viewbtn{
  width:34px;height:34px;border-radius:var(--r-pill);border:0;padding:0;flex:0 0 auto;
  display:grid;place-items:center;background:var(--surface-2);color:var(--on-surface-v);
  transition:transform .14s,background .16s,color .16s;
}
.viewbtn:active{transform:scale(.92);background:var(--surface-3);color:var(--on-surface)}
.viewbtn svg{width:17px;height:17px}
.chip{
  display:inline-flex;align-items:center;gap:6px;
  padding:6px 12px 6px 9px;border-radius:var(--r-pill);
  background:var(--surface-2);color:var(--on-surface-v);
  font-size:12.5px;font-weight:560;
}
.chip svg{width:15px;height:15px}
.chip.ok{background:var(--ok-c);color:var(--ok)}
.chip.wait{background:var(--primary-c);color:var(--on-primary-c)}
.chip.bad{background:var(--bad-c);color:var(--bad)}

/* ---------- 列表 ---------- */
.list{padding:12px 14px 8px;display:flex;flex-direction:column;gap:11px}
.card{
  background:var(--card);border-radius:var(--r-xl);
  padding:16px 17px;box-shadow:var(--shadow);
  border:1px solid var(--outline-v);
  will-change:transform;   /* 提前提层，避免动画时再合成导致掉帧 */
  animation:rise .32s cubic-bezier(.2,.8,.2,1) both;
}
@keyframes rise{from{opacity:0;transform:translateY(10px) scale(.985)}to{opacity:1;transform:none}}
.card:active{transform:scale(.988);transition:transform .12s}
.card.err{border-color:color-mix(in srgb,var(--bad) 40%,var(--outline-v))}
.c-top{display:flex;align-items:center;gap:10px;margin-bottom:0}
.avatar{
  width:38px;height:38px;border-radius:12px;display:grid;place-items:center;
  font-size:15px;font-weight:700;color:#fff;flex:0 0 auto;
  box-shadow:0 3px 10px rgba(0,0,0,.14);
}
.c-name{flex:1;min-width:0}
.c-name .p{font-size:15px;font-weight:640;letter-spacing:-.01em;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.c-name .s{font-size:12.5px;color:var(--on-surface-v);margin-top:1px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
/* 余额紧贴在标题那一行的右端，不再单独占一行并左缩进 48px。
   原来余额行 margin-left:48px，导致头像下沿到卡片左侧出现一条空带，
   视觉上余额像是悬空的（用户原话「左侧为什么还有那么多空白」）。
   现在余额、标题、··· 同处一行，右侧一列对齐，卡片高度也少了约 30px。 */
.balance{display:flex;align-items:baseline;gap:4px;font-variant-numeric:tabular-nums;
  margin-left:auto;flex:0 0 auto;padding-left:6px}
.balance .n{font-size:20px;font-weight:700;letter-spacing:-.025em}
.balance .u{font-size:11.5px;color:var(--on-surface-v);font-weight:560}
.c-foot{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-top:11px;
  padding-top:11px;border-top:1px solid var(--outline-v)}
.status{display:flex;align-items:center;gap:6px;font-size:12.5px;color:var(--on-surface-v);min-width:0}
/* 状态文案现在带「+N 单位」，比原来长，给按钮组留出固定宽度后允许省略 */
.status{flex:1}
.status .txt{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;flex:1;min-width:0}
.status.ok{color:var(--ok)}.status.bad{color:var(--bad)}
.status svg{width:15px;height:15px}
.pill{
  border:0;border-radius:var(--r-pill);padding:8px 15px;font-size:13px;font-weight:640;
  background:var(--primary);color:var(--on-primary);
  transition:transform .14s,opacity .14s,background .14s;
  display:inline-flex;align-items:center;gap:6px;
  /* 按钮永不被压缩：长错误文案会挤压同行元素，flex 默认会把它压成两行 */
  flex:0 0 auto;white-space:nowrap;
}
.pill svg{width:16px;height:16px}
.pill:active{transform:scale(.95)}
.pill[disabled]{opacity:.4}
.pill.done{background:var(--ok-c);color:var(--ok)}
.pill.fail{background:var(--bad-c);color:var(--bad)}
/* 任务按钮：与签到并排，用低调的描边色区分主次 */
.pill.task{background:var(--surface-2);color:var(--on-surface-v)}
.pill.task:active{background:var(--surface-3)}
.c-actions{display:flex;align-items:center;gap:8px;flex:0 0 auto}

/* 紧凑视图：一行一个账号。
   用户反馈「平铺的卡片太大、太占地方」，卡片视图保留（信息全），
   紧凑视图给「只想扫一眼签没签」的场景，一屏能装下十几个账号。 */
.list.compact{gap:7px}
.row-item{
  display:flex;align-items:center;gap:9px;
  background:var(--card);border:1px solid var(--outline-v);
  border-radius:var(--r-md);padding:8px 11px;box-shadow:var(--shadow);
  animation:rise .26s cubic-bezier(.2,.8,.2,1) both;
}
.row-item .avatar{width:30px;height:30px;border-radius:9px;font-size:12.5px}
.row-main{flex:1;min-width:0;display:flex;flex-direction:column;gap:0}
.row-main .t{font-size:13px;font-weight:620;letter-spacing:-.01em;
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.row-main .d{font-size:11px;color:var(--on-surface-v);margin-top:1px;
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.row-main .d.bad{color:var(--bad)}
/* 副标题里的余额比说明文字重一档，但不抢标题的主次 */
.row-main .d .d-bal{font-size:12.5px;font-weight:700;color:var(--on-surface);
  font-variant-numeric:tabular-nums;letter-spacing:-.01em}
.row-main .d .d-sep{color:var(--on-surface-v)}
.row-act{flex:0 0 auto;display:flex;align-items:center;gap:4px}
/* 紧凑视图里的操作按钮做成小号胶囊，高度和行高对齐。
   0.6.2：任务/抽奖不再用图标——图标在 30px 的方框里既占宽又看不出是什么，
   换成同样宽度的文字反而更好认（用户原话「那个图标有个鸟用」）。 */
.row-act .pill{padding:4px 9px;font-size:11.5px;border-radius:var(--r-pill);gap:0}
.row-act .pill.mute{background:var(--surface-2);color:var(--on-surface-v)}
.row-item.err .row-main .t{color:var(--bad)}

/* 拖动排序：长按任意卡片后可上下拖动更换位置 */
.card, .row-item{cursor:grab}
.card.longpressed, .row-item.longpressed{
  box-shadow:var(--shadow-lg);
  transform:scale(1.02);
  border-color:color-mix(in srgb,var(--primary) 55%,var(--outline-v));
  z-index:2;position:relative;
  transition:transform .16s,box-shadow .16s;
}
.card.dragging, .row-item.dragging{opacity:.92}
body.reordering{cursor:grabbing}
body.reordering *{user-select:none!important;-webkit-user-select:none!important}
/* 拖动时让出位的那一行，用虚线占位提示 */
.card.drop-slot, .row-item.drop-slot{
  border:1px dashed color-mix(in srgb,var(--primary) 45%,transparent);
  background:color-mix(in srgb,var(--primary) 6%,var(--card));
}
.order-tip{
  font-size:11.5px;color:var(--on-surface-v);text-align:center;
  padding:0 0 4px;
}

/* 骨架屏：首屏立刻有结构，避免白屏等待的「卡」感 */
.skel{height:118px;border-radius:var(--r-xl);background:var(--card);border:1px solid var(--outline-v);
      position:relative;overflow:hidden}
.skel::after{content:"";position:absolute;inset:0;
  background:linear-gradient(90deg,transparent,var(--surface-3),transparent);
  transform:translateX(-100%);animation:sheen 1.15s infinite}
@keyframes sheen{to{transform:translateX(100%)}}

/* ---------- 下拉刷新 ---------- */
.puller{
  position:fixed;left:50%;top:calc(4px + var(--sat));z-index:25;
  width:38px;height:38px;border-radius:50%;display:grid;place-items:center;
  background:var(--card);box-shadow:var(--shadow-lg);color:var(--primary);
  transform:translate(-50%,-70px);opacity:0;pointer-events:none;
  transition:transform .26s cubic-bezier(.2,.8,.2,1),opacity .2s;
}
.puller.on{transform:translate(-50%,0);opacity:1}
.puller svg{width:19px;height:19px;transition:transform .2s}
.puller.ready svg{transform:rotate(180deg)}
.puller.busy svg{animation:spin .75s linear infinite}

/* ---------- 空态 / 错误态 ---------- */
.empty{
  margin:auto 24px;padding:24px 0;text-align:center;color:var(--on-surface-v);
  display:flex;flex-direction:column;align-items:center;gap:14px;
}
.empty .ring{
  width:74px;height:74px;border-radius:50%;display:grid;place-items:center;
  background:var(--surface-2);color:var(--primary);
}
.empty .ring svg{width:32px;height:32px}
.empty h2{font-size:16px;color:var(--on-surface);margin:0;font-weight:640}
.empty p{font-size:13.5px;margin:0;line-height:1.6;max-width:270px}

/* ---------- 底部操作条 ----------
   只有两个动作（一键签到 / 添加账号）：签到把领任务和抽奖一起做完，
   所以不再需要第三个按钮。按钮也因此可以更矮、更紧凑，少占一截屏幕。 */
.bar{
  position:fixed;left:0;right:0;bottom:0;z-index:28;
  padding:8px 14px calc(8px + var(--sab));
  background:color-mix(in srgb,var(--surface) 90%,transparent);
  backdrop-filter:saturate(180%) blur(18px);-webkit-backdrop-filter:saturate(180%) blur(18px);
  border-top:1px solid var(--outline-v);
  display:flex;gap:9px;
}
.btn{
  flex:1;border:0;border-radius:var(--r-pill);padding:11px 14px;
  font-size:14.5px;font-weight:640;letter-spacing:.01em;
  display:inline-flex;align-items:center;justify-content:center;gap:7px;
  transition:transform .14s,opacity .14s;
}
.btn:active{transform:scale(.975)}
.btn[disabled]{opacity:.45}
.btn.tonal{background:var(--surface-2);color:var(--on-surface)}
.btn.filled{background:var(--primary);color:var(--on-primary);box-shadow:0 6px 18px color-mix(in srgb,var(--primary) 32%,transparent)}
.btn svg{width:18px;height:18px}

/* ---------- 底部弹层（添加账号） ---------- */
.mask{
  position:fixed;inset:0;background:rgba(12,10,20,.46);z-index:50;
  opacity:0;transition:opacity .22s;
}
.mask.on{opacity:1}
.sheet{
  position:fixed;left:0;right:0;bottom:0;z-index:52;
  background:var(--card);
  isolation:isolate;border-radius:var(--r-xl) var(--r-xl) 0 0;
  padding:20px 16px calc(14px + var(--sab));
  transform:translateY(102%);transition:transform .3s cubic-bezier(.2,.8,.2,1);
  box-shadow:0 -10px 40px rgba(0,0,0,.22);
  max-height:82vh;overflow-y:auto;-webkit-overflow-scrolling:touch;
}
.sheet.on{transform:none}
.sheet-h{font-size:17px;font-weight:680;letter-spacing:-.01em;padding:0 4px}
.sheet-sub{font-size:13px;color:var(--on-surface-v);padding:6px 4px 14px;line-height:1.55}
.sheet-item{
  display:flex;align-items:center;gap:12px;width:100%;
  border:0;background:var(--surface-1);border-radius:var(--r-lg);
  padding:12px 14px;margin-bottom:9px;text-align:left;color:var(--on-surface);
  transition:transform .14s,background .14s;
}
.sheet-item:active{transform:scale(.98);background:var(--surface-3)}
.sheet-dot{
  width:36px;height:36px;border-radius:11px;flex:0 0 auto;
  display:grid;place-items:center;color:#fff;font-weight:700;font-size:15px;
}
.sheet-txt{flex:1;min-width:0;display:flex;flex-direction:column;gap:1px}
.sheet-txt b{font-size:14.5px;font-weight:640}
.sheet-txt i{font-style:normal;font-size:12px;color:var(--on-surface-v);
  overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.sheet-arw{color:var(--on-surface-v);flex:0 0 auto}
.sheet-arw svg{width:18px;height:18px}
.sheet-cancel{
  width:100%;border:0;background:transparent;color:var(--on-surface-v);
  padding:14px;font-size:14.5px;font-weight:600;border-radius:var(--r-md);
}
.sheet-cancel:active{background:var(--surface-2)}

/* ---------- 表单 / 设置 ---------- */
.field{display:flex;flex-direction:column;gap:6px;margin-bottom:12px}
.field > label{font-size:12.5px;font-weight:600;color:var(--on-surface-v);padding-left:2px}
.field input,.field textarea,.field select{
  width:100%;border:1px solid var(--outline-v);border-radius:var(--r-md);
  background:var(--surface-1);color:var(--on-surface);
  font:14px/1.5 -apple-system,BlinkMacSystemFont,"PingFang SC",system-ui,sans-serif;
  padding:11px 13px;outline:none;
}
.field input:focus,.field textarea:focus,.field select:focus{border-color:var(--primary)}
.field textarea{resize:vertical;min-height:88px}
.field textarea.mono{
  font:12px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  min-height:150px;word-break:break-all;
}
.field .tip{font-size:11.5px;color:var(--on-surface-v);line-height:1.5;padding-left:2px}
.row-btns{display:flex;gap:10px;margin:4px 0 10px}
.row-btns .btn{flex:1}
.btn.ghost{background:var(--surface-2);color:var(--on-surface);box-shadow:none}
.btn.danger{background:var(--bad-c);color:var(--bad);box-shadow:none}
.btn.sm{padding:10px 14px;font-size:13.5px}

/* 账号管理行 */
.acc-row{
  display:flex;align-items:center;gap:10px;padding:11px 12px;
  background:var(--surface-1);border-radius:var(--r-md);margin-bottom:8px;
}
.acc-row .dot{width:30px;height:30px;border-radius:9px;display:grid;place-items:center;
  color:#fff;font-weight:700;font-size:12px;flex:0 0 auto}
.acc-row .info{flex:1;min-width:0;display:flex;flex-direction:column;gap:1px}
.acc-row .info b{font-size:13.5px;font-weight:620;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.acc-row .info i{font-style:normal;font-size:11.5px;color:var(--on-surface-v);
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.acc-row.off{opacity:.55}
.mini{
  border:0;border-radius:var(--r-pill);padding:7px 12px;font-size:12.5px;font-weight:620;
  background:var(--surface-2);color:var(--on-surface-v);flex:0 0 auto;white-space:nowrap;
}
.mini:active{transform:scale(.95)}
.mini.del{background:var(--bad-c);color:var(--bad)}
.mini.go{background:var(--primary-c);color:var(--on-primary-c)}
/* 每日任务行 */
.task-row{
  display:flex;align-items:center;gap:10px;padding:11px 12px;
  background:var(--surface-1);border-radius:var(--r-md);margin-bottom:8px;
}
.task-info{flex:1;min-width:0;display:flex;flex-direction:column;gap:2px}
.task-title{font-size:13.5px;font-weight:620;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.task-sub{font-size:11.5px;color:var(--on-surface-v);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
/* 抽奖弹层 */
.lot-card{
  background:linear-gradient(135deg,color-mix(in srgb,var(--primary) 14%,var(--card)),var(--card));
  border:1px solid var(--outline-v);border-radius:var(--r-lg);
  padding:16px;margin-bottom:12px;text-align:center;
}
.lot-cap{font-size:12.5px;color:var(--on-surface-v);margin-bottom:2px}
.lot-remain{
  font-size:38px;font-weight:720;letter-spacing:-.03em;line-height:1.1;
  color:var(--on-surface-v);font-variant-numeric:tabular-nums;
}
.lot-remain.on{
  background:linear-gradient(120deg,var(--primary),#8B4DFF 90%);
  -webkit-background-clip:text;background-clip:text;-webkit-text-fill-color:transparent;
}
.lot-prizes{margin-bottom:12px}
.lot-empty{font-size:12.5px;color:var(--on-surface-v);text-align:center;padding:10px 0 14px}
.lot-prize{
  display:flex;align-items:center;gap:10px;padding:10px 12px;
  background:var(--surface-1);border-radius:var(--r-md);margin-bottom:8px;
  animation:rise .28s cubic-bezier(.2,.8,.2,1) both;
}
.lot-ic{
  width:30px;height:30px;border-radius:9px;flex:0 0 auto;display:grid;place-items:center;
  background:var(--warn-c);color:var(--warn);
}
.lot-ic svg{width:17px;height:17px}
.lot-tx{flex:1;min-width:0;display:flex;flex-direction:column}
.lot-tx b{font-size:13.5px;font-weight:640;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.lot-tx i{font-style:normal;font-size:11.5px;color:var(--ok);margin-top:1px}
/* 抽奖按钮：和「任务」同一种低调底色。
   0.6.2 之前抽奖单独用黄色（--warn-c），一张卡片上蓝（签到）、绿（已签）、
   黄（抽奖）三种彩色按钮并排，看着很花，也没有理由把抽奖单独强调出来。
   现在只保留一个彩色按钮（签到），其余都用表面色。 */
.pill.lottery{background:var(--surface-2);color:var(--on-surface-v)}
.pill.lottery:active{background:var(--surface-3)}
.sect-h{font-size:13px;font-weight:680;color:var(--on-surface-v);
  margin:18px 4px 10px;letter-spacing:.02em}
.sect-h:first-child{margin-top:6px}
.notice{
  background:var(--warn-c);color:var(--warn);border-radius:var(--r-md);
  padding:10px 12px;font-size:12.5px;line-height:1.55;margin-bottom:12px;
}
/* 中性说明条：只是解释「这是什么」，没有风险含义，不该用警示色抢注意力 */
.notice.soft{background:var(--surface-2);color:var(--on-surface-v)}
.meta{font-size:12px;color:var(--on-surface-v);text-align:center;padding:14px 0 4px}
/* ---- 运行日志 ----
   等宽字体 + 每条一行：用户扫一眼能对上是「什么时间 / 哪个请求 / 什么状态」。
   失败条目用左侧色条区分级别，不靠颜色文字（避免只有红绿难辨）。 */
.logbox{
  background:var(--surface-2);border-radius:var(--r-md);padding:4px 0;
  max-height:46vh;overflow:auto;-webkit-overflow-scrolling:touch;
}
.logrow{
  display:flex;gap:7px;align-items:baseline;padding:6px 11px;
  font-family:var(--mono);font-size:11.5px;line-height:1.45;
  border-left:3px solid transparent;
}
.logrow+.logrow{border-top:1px solid var(--hair)}
/* 时间固定宽度：现在带毫秒（HH:MM:SS.mmm），不定宽会让每行的正文起点参差 */
.logrow .lt{color:var(--on-surface-v);flex:0 0 82px;font-variant-numeric:tabular-nums;letter-spacing:-.02em}
.logrow .lm{flex:1;min-width:0;word-break:break-word;color:var(--on-surface)}
/* 步骤名不折行：一个步骤一条记录，折成两行会把一屏能看的条数砍掉一半 */
.logrow .lm b{font-weight:650;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;display:block}
.logrow .lc{flex:none;color:var(--on-surface-v);font-variant-numeric:tabular-nums}
.logrow.warn{border-left-color:var(--warn);background:var(--warn-c)}
.logrow.error{border-left-color:var(--bad);background:var(--bad-c)}
.logrow.error .lm{color:var(--bad)}
.logempty{padding:18px 12px;text-align:center;color:var(--on-surface-v);font-size:12.5px}
.logrow .sub{color:var(--on-surface-v);font-size:11px}

/* 分段控件（主题三选一） */
.seg{
  display:flex;gap:4px;padding:4px;margin-bottom:12px;
  background:var(--surface-2);border-radius:var(--r-pill);
}
.seg-item{
  flex:1;border:0;background:transparent;color:var(--on-surface-v);
  border-radius:var(--r-pill);padding:9px 8px;font-size:13px;font-weight:600;
  transition:background .16s,color .16s;
}
.seg-item.on{background:var(--card);color:var(--on-surface);box-shadow:var(--shadow)}
.seg-item:active{transform:scale(.97)}
/* 通知页的渠道选择（0.6.5）：5 个渠道要换行成两三排，
   999px 的胶囊圆角在换行后每条都变成一颗橄榄球，弧度大得离谱。
   这里单独收成 12px 外框 / 9px 内块的小圆角，跟下面的输入框对齐。 */
.seg.wrap{border-radius:12px;padding:5px;gap:5px;margin-bottom:12px}
.seg.wrap .seg-item{border-radius:9px;padding:8px 6px;font-size:12.5px}

/* ---- 设置页分页签（0.6.4） ----
   四个页签横向等分，切页时下方内容整体替换。没有二级弹层：
   用户抱怨过「设置里点进去又弹一层，还得再退回来」，这里一页到底。 */
.tab-pane{padding-top:2px}
/* 把若干开关收进一张卡，省掉四个 sect-h 带来的空白 */
.sect-card{
  background:var(--surface-1);border-radius:var(--r-lg);
  padding:4px 12px;margin-bottom:6px;
}
.sect-card .chk{background:transparent;border-radius:0;padding:10px 0}
.sect-card .chk+.chk{border-top:1px solid var(--hair)}
/* 账号 Tab 的行：头像 + 两行文字 + 箭头，整行可点 */
.nav-row{
  display:flex;align-items:center;gap:11px;width:100%;
  border:0;background:var(--surface-1);border-radius:var(--r-lg);
  padding:11px 13px;margin-bottom:8px;text-align:left;color:var(--on-surface);
  transition:transform .14s,background .14s;
}
.nav-row:active{transform:scale(.985);background:var(--surface-3)}
.nav-row.off{opacity:.55}
.nav-row .dot{
  width:34px;height:34px;border-radius:11px;flex:0 0 auto;
  display:grid;place-items:center;color:#fff;font-weight:700;font-size:14px;
}
.acc-info{flex:1;min-width:0;display:flex;flex-direction:column;gap:1px}
.acc-info b{font-size:14.5px;font-weight:640;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.acc-info i{font-style:normal;font-size:12px;color:var(--on-surface-v);
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.nav-ic{color:var(--on-surface-v);display:grid;place-items:center;flex:0 0 auto}
.nav-ic svg{width:19px;height:19px}
.nav-go{color:var(--on-surface-v);flex:0 0 auto;display:grid;place-items:center}
.nav-go svg{width:18px;height:18px}

/* ---- 账号 Tab 的行内操作（0.6.5） ----
   以前整行可点，进了详情页就退不回来（用户原话「还得再从设置里进去一遍」）。
   现在整行不可点，三个操作直接摆在右边：令牌 / 停用 / 删除。 */
.acc-row{
  display:flex;align-items:center;gap:10px;
  background:var(--surface-1);border-radius:var(--r-lg);
  padding:10px 12px;margin-bottom:8px;
}
.acc-row .dot{
  width:32px;height:32px;border-radius:10px;flex:0 0 auto;
  display:grid;place-items:center;color:#fff;font-weight:700;font-size:13px;
}
.acc-row.off .dot,.acc-row.off .acc-info{opacity:.5}
.acc-acts{display:flex;gap:5px;flex:0 0 auto}
.a-btn{
  border:0;border-radius:9px;padding:9px 10px;line-height:1;
  font-size:11.5px;font-weight:650;letter-spacing:.01em;
  background:var(--surface-3);color:var(--on-surface);
  transition:transform .12s,background .16s;
}
.a-btn:active{transform:scale(.94)}
.a-btn:disabled{opacity:.5}
.a-btn.key{background:var(--primary-c);color:var(--on-primary-c)}
.a-btn.warn{background:var(--bad-c);color:var(--bad)}
/* 确认删：反色高亮。文字色必须用 --surface 而不是 #fff——
   深色主题下 --bad 是浅粉（#FFB4AB），白字在上面根本看不清。 */
.a-btn.arm{background:var(--bad);color:var(--surface);box-shadow:0 0 0 2px var(--bad-c)}
/* 底部说明：把三个按钮的意思写清楚，省掉「点进去才知道」 */
.acc-note{
  margin-top:14px;padding:11px 13px;border-radius:var(--r-md);
  background:var(--surface-2);font-size:11.5px;line-height:1.65;color:var(--on-surface-v);
}
.acc-note div+div{margin-top:7px}
.acc-note b{color:var(--on-surface);font-weight:660}

/* ---- 令牌列表（0.6.0） ----
   一条令牌 = 名称 + 完整 key + 额度；操作排在卡片底部一行。
   为什么不做表格：手机上一篇 12 列的表没人看得下去，竖排两行最省眼。 */
.tok{
  background:var(--surface-2);border-radius:var(--r-md);
  padding:11px 13px;margin-bottom:9px;
}
.tok.off{opacity:.55}
.tok-h{display:flex;align-items:baseline;gap:8px}
.tok-h b{font-size:14px;font-weight:650;letter-spacing:-.01em;
  flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.tok-h .st{font-size:11px;font-weight:640;padding:1px 7px;border-radius:var(--r-pill);
  background:var(--ok-c);color:var(--ok);flex:0 0 auto}
.tok.off .tok-h .st{background:var(--bad-c);color:var(--bad)}
/* key 本体：完整一行，等宽字体，长 key 折行也不截断。
   0.6.2：复制按钮移到下方 tok-act 里和「禁用/删除」同排——
   之前它挂在 key 行右端单独一个位置，一列按钮被拆成两处，看着很散。 */
.tok-key{
  font-family:var(--mono);font-size:12px;color:var(--on-surface);
  margin-top:5px;word-break:break-all;line-height:1.5;
  padding:6px 9px;border-radius:8px;
  background:var(--surface-1);
}
.tok-meta{font-size:11.5px;color:var(--on-surface-v);margin-top:3px}
.tok-meta b{font-weight:620;color:var(--on-surface)}
.tok-lim{font-size:11px;color:var(--on-surface-v);margin-top:2px;word-break:break-all}
.tok-act{display:flex;gap:6px;margin-top:9px;flex-wrap:wrap}
.tok-btn{
  border:0;border-radius:var(--r-pill);padding:6px 12px;font-size:12px;font-weight:620;
  background:var(--surface-3);color:var(--on-surface);transition:transform .14s;
}
.tok-btn:active{transform:scale(.95)}
.tok-btn.danger{color:var(--bad)}

/* 通知表单里的复选框行 */
.chk{
  display:flex;align-items:center;gap:11px;padding:11px 2px;
  border-bottom:1px solid var(--hair);
}
.chk:last-of-type{border-bottom:0}
.chk-t{flex:1;min-width:0}
.chk-t b{display:block;font-size:13.5px;font-weight:600}
.chk-t i{display:block;font-style:normal;font-size:11.5px;color:var(--on-surface-v);
  margin-top:1px;line-height:1.4}
.sw{
  width:44px;height:26px;border-radius:var(--r-pill);background:var(--surface-3);
  border:0;position:relative;flex:0 0 auto;transition:background .18s;
}
.sw::after{
  content:'';position:absolute;top:3px;left:3px;width:20px;height:20px;border-radius:50%;
  background:var(--card);box-shadow:0 1px 3px rgba(0,0,0,.28);
  transition:transform .2s cubic-bezier(.2,.8,.2,1);
}
.sw.on{background:var(--primary)}
.sw.on::after{transform:translateX(18px)}

/* 设置页里的「跳转行」：右箭头 + 一行说明 */
.nav-row{
  display:flex;align-items:center;gap:12px;width:100%;border:0;
  background:var(--surface-1);color:var(--on-surface);border-radius:var(--r-lg);
  padding:12px 14px;margin-bottom:9px;text-align:left;
}
.nav-row:active{transform:scale(.98);background:var(--surface-3)}
.nav-ic{width:32px;height:32px;border-radius:10px;flex:0 0 auto;
  display:grid;place-items:center;background:var(--surface-3);color:var(--on-surface-v)}
.nav-ic svg{width:17px;height:17px}
.nav-t{flex:1;min-width:0}
.nav-t b{display:block;font-size:14px;font-weight:640}
.nav-t i{display:block;font-style:normal;font-size:11.5px;color:var(--on-surface-v);
  margin-top:1px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.nav-go{color:var(--on-surface-v);flex:0 0 auto}
.nav-go svg{width:17px;height:17px}

/* 概览触发行：点击查看本轮汇总 / 历史 */
.hero-tap{cursor:pointer}

/* Snackbar */
.snack{
  position:fixed;left:50%;bottom:calc(88px + var(--sab));transform:translate(-50%,24px);
  background:var(--on-surface);color:var(--surface);
  padding:12px 20px;border-radius:var(--r-md);font-size:13.5px;font-weight:540;
  box-shadow:var(--shadow-lg);opacity:0;pointer-events:none;z-index:40;
  transition:transform .26s cubic-bezier(.2,.8,.2,1),opacity .26s;
  /* pill 形态：不换行、宽度随内容、超长才省略。之前固定 86vw 会让
     「完成：新签 1，已签 0，失败 4」折成两行，视觉上像一块压在卡片上的补丁。 */
  width:max-content;max-width:min(88vw,520px);
  white-space:nowrap;overflow:hidden;text-overflow:ellipsis;text-align:center;
}
.snack.on{transform:translate(-50%,0);opacity:1}

/* 尊重系统「减少动态效果」 */
@media (prefers-reduced-motion:reduce){
  *{animation-duration:.001s!important;transition-duration:.001s!important}
}
</style></head><body>

<div class="puller" id="puller">{{ICON:refresh}}</div>

<div class="appbar" id="appbar">
  <div class="brand">
    <div class="logo">{{ICON:spark}}</div>
    <h1>积分矩阵</h1>
  </div>
  <button class="iconbtn" id="btnRefresh" aria-label="刷新">{{ICON:refresh}}</button>
  <button class="iconbtn" id="btnTheme" aria-label="切换主题模式">{{ICON:sun}}</button>
  <button class="iconbtn" id="btnSettings" aria-label="设置">{{ICON:gear}}</button>
</div>

<!-- 活动条：把「后台正在做什么」明明白白写在最上面。
     用户的原话：「我都不知道状态」「谁前签谁后签、还是没签、还是后台在签前台没刷新」。
     所以签到的每一步都往这里写一行，做完转绿、失败转红，然后自动收起。 -->
<div class="act" id="act">
  <span class="act-dot"></span>
  <span class="act-txt" id="actTxt"></span>
</div>

<div class="hero">
  <div class="hero-head" id="hero-head">
    <div class="kicker">全部账号合计</div>
    <div class="total" id="total-row"><span class="num" id="total">—</span><span class="unit" id="total-unit">积分</span></div>
  </div>
  <!-- 没有总数时（无账号 / 全部异常 / 单位不同）整行大字隐藏。
       以前这里会补一句「单位不同所以不合计」，用户明确说没用又占地方，已删除。 -->
  <div class="chips-row">
    <div class="chips" id="chips"></div>
    <!-- 签到逻辑说明入口：就放在「N 个正常 / N 个已签」右边，用户一眼能找到 -->
    <button class="viewbtn" id="btnHelp" aria-label="签到逻辑说明">{{ICON:info}}</button>
    <button class="viewbtn" id="btnView" aria-label="切换显示密度">{{ICON:list}}</button>
  </div>
</div>

<div class="list" id="list"></div>

<div class="bar">
  <button class="btn tonal" id="btnAll" disabled>{{ICON:checkAll}}一键签到</button>
  <button class="btn filled" id="btnAdd">{{ICON:plus}}添加账号</button>
</div>

<div class="snack" id="snack"></div>

<script>
(function(){
  "use strict";
  var $ = function(id){ return document.getElementById(id); };
  var state = { rows: [], busy: false, providers: [], meta: null };

  /* ---------- 工具 ---------- */
  function fmt(n){
    n = Number(n) || 0;
    // 整数不显示小数，避免「1,500.00 积分」这种冗余
    return n.toLocaleString('zh-CN', { maximumFractionDigits: n % 1 ? 1 : 0 });
  }
  // 余额专用：小额（<1）保留两位，至少让「0.04 灵石」不显示成「0」。
  function fmtMoney(n){
    n = Number(n) || 0;
    if (n > 0 && n < 1) return (Math.round(n * 100) / 100).toString();
    return fmt(n);
  }
  function esc(s){
    return String(s == null ? '' : s).replace(/[&<>"']/g, function(c){
      return ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'})[c];
    });
  }
  var HUES = [232, 268, 320, 8, 42, 168, 196, 148];
  function hueOf(id){
    var h = 0; id = String(id || '');
    for (var i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) % 360;
    return HUES[h % HUES.length];
  }
  function initials(name){
    var s = String(name || '?').trim();
    // 中文取首字，英文取首字母
    return /[\u4e00-\u9fa5]/.test(s) ? s.slice(0, 1) : s.slice(0, 1).toUpperCase();
  }
  var snackTimer;
  function toast(msg){
    var el = $('snack'); el.textContent = msg; el.classList.add('on');
    clearTimeout(snackTimer);
    snackTimer = setTimeout(function(){ el.classList.remove('on'); }, 2400);
  }
  function j(u, o){
    // 带超时的 fetch：弱网/平台接口挂住时不让骨架屏无限转，超时给出可操作的提示。
    // 引擎侧每个账号 20s 超时且并行，这里留 25s 余量。
    //
    // 0.6.4 删掉了「网络层失败自动重试一次」。
    // 那是 0.6.3 为「回前台读取失败」加的措施，实质什么都没改变——真正的
    // 原因是 Go 侧复用了已被系统回收的 keep-alive 连接（已在 httpx.New 里
    // 改成 DisableKeepAlives）。重试只是把一次失败变成两次失败 + 500ms 卡顿，
    // 顺带让「读取失败」来得更晚。现在失败就是失败，界面给「重试」按钮，
    // 由用户决定（用户原话：「你现有的措施全部删掉」）。
    var ctrl = (typeof AbortController !== 'undefined') ? new AbortController() : null;
    var opt = o || {};
    // 每个请求都带本机 UTC 偏移（东八区 = -480）。Go 在手机进程里读不到系统时区，
    // 日志时间戳全按 UTC 记，会比本地慢 8 小时。服务端 middleware 收到这个头后
    // 换算日志时间。不能只在启动时报一次：进程重建 / WebView 重新加载后内存值会丢。
    opt.headers = opt.headers || {};
    opt.headers['X-TZ-Offset'] = String(new Date().getTimezoneOffset());
    // 0.6.9：POST body 双保险。iOS 走 pointsapi:// 自定义 scheme 时，
    // WKURLSchemeHandler 的 httpBody 正常会带上；这里额外把 body 编码进 header
    // 让原生层兜底读取（桌面 HTTP server 不认这个 header，自动忽略，无副作用）。
    if (opt.body) opt.headers['X-Points-Body'] = encodeURIComponent(opt.body);
    var timer = setTimeout(function(){ if (ctrl) ctrl.abort(); }, 25000);
    if (ctrl) opt.signal = ctrl.signal;
    return fetch(u, opt).then(function(r){
      if (!r.ok) throw new Error('HTTP ' + r.status);
      return r.json();
    }).catch(function(e){
      if (e && e.name === 'AbortError') throw new Error('请求超时，请检查网络后重试');
      throw e;
    }).then(function(v){ clearTimeout(timer); return v; },
            function(e){ clearTimeout(timer); throw e; });
  }

  /* ---------- 通用 UI 工具 ---------- */
  // POST JSON：账号增删改都走这个，避免每处重复拼 headers
  function post(url, body){
    return j(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {})
    });
  }
  function el(tag, cls, text){
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }
  function platformName(id){
    for (var i = 0; i < state.providers.length; i++){
      if (state.providers[i].id === id) return state.providers[i].name || id;
    }
    return id || '';
  }
  // 通用底部弹层。build(body, close) 里往 body 塞内容。
  // opts.hideCancel：隐藏底部「关闭」按钮（添加账号 / 设置 / 签到逻辑页用，点遮罩仍可关闭）。
  // opts.hideTitle：隐藏顶部标题栏（设置页要求「设置」两字不显示）。
  function openSheet(title, subtitle, build, opts){
    opts = opts || {};
    var mask = el('div', 'mask');
    var sheet = el('div', 'sheet');
    if (!opts.hideTitle) sheet.appendChild(el('div', 'sheet-h', title));
    if (subtitle) sheet.appendChild(el('div', 'sheet-sub', subtitle));
    var body = el('div', '');
    sheet.appendChild(body);
    var cancel = null;
    if (!opts.hideCancel){
      cancel = el('button', 'sheet-cancel', '关闭');
      sheet.appendChild(cancel);
    }
    document.body.appendChild(mask);
    document.body.appendChild(sheet);
    requestAnimationFrame(function(){
      mask.classList.add('on'); sheet.classList.add('on');
    });
    function close(){
      mask.classList.remove('on'); sheet.classList.remove('on');
      setTimeout(function(){
        if (mask.parentNode) mask.parentNode.removeChild(mask);
        if (sheet.parentNode) sheet.parentNode.removeChild(sheet);
      }, 240);
    }
    mask.addEventListener('click', close);
    if (cancel) cancel.addEventListener('click', close);
    if (build) build(body, close);
    return { el: sheet, body: body, close: close };
  }
  // 表单字段：label + 输入框 + 可选说明
  function mkField(labelText, tag, attrs, tip){
    var f = el('div', 'field');
    f.appendChild(el('label', '', labelText));
    var inp = document.createElement(tag);
    for (var k in attrs) inp.setAttribute(k, attrs[k]);
    f.appendChild(inp);
    if (tip) f.appendChild(el('div', 'tip', tip));
    return { wrap: f, input: inp };
  }
  // 平台下拉
  function mkPlatformSelect(defaultId){
    var f = el('div', 'field');
    f.appendChild(el('label', '', '平台'));
    var sel = document.createElement('select');
    (state.providers.length ? state.providers : [{id:'workbuddy',name:'WorkBuddy'}]).forEach(function(p){
      var o = document.createElement('option');
      o.value = p.id; o.textContent = p.name || p.id;
      sel.appendChild(o);
    });
    if (defaultId) sel.value = defaultId;
    f.appendChild(sel);
    return { wrap: f, input: sel };
  }
  // 每行 key=value 的高级字段
  function parseKV(txt){
    var out = {};
    String(txt || '').split(/\n/).forEach(function(line){
      var m = line.match(/^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*(.*)\s*$/);
      if (m) out[m[1]] = m[2];
    });
    return out;
  }
  // 复制：WKWebView / Android WebView 里 clipboard API 不一定可用，
  // 用 execCommand 兜底，再不行就提示用户长按全选。
  function copyText(ta){
    if (!ta.value){ toast('先生成备份'); return; }
    var ok = false;
    try {
      ta.focus(); ta.select(); ta.setSelectionRange(0, ta.value.length);
      ok = document.execCommand('copy');
    } catch (e) {}
    if (ok){ toast('已复制到剪贴板'); return; }
    if (navigator.clipboard && navigator.clipboard.writeText){
      navigator.clipboard.writeText(ta.value)
        .then(function(){ toast('已复制到剪贴板'); })
        .catch(function(){ toast('请长按文本框全选后复制'); });
    } else {
      toast('请长按文本框全选后复制');
    }
  }

  // copyToken：复制一个字符串（令牌 key）。与 copyText 分开是因为那个只吃 textarea。
  //
  // 三层兜底，顺序有讲究：
  //   1. navigator.clipboard —— 现代实现最干净，但 iOS 要求「用户手势直接触发」
  //      且页面是安全上下文；本机回环地址在 WKWebView 里算安全，
  //      可一旦被判定不是手势（比如 Promise 链里调用）就会 rejected。
  //   2. 隐藏 textarea + execCommand('copy') —— WKWebView 上历史最稳的路径，
  //      必须真的插入 DOM 并 select()，否则 iOS 复制不到。
  //   3. 都不行就把 key 显示在一个可长按选中的 input 里，让用户自己复制，
  //      而不是只弹一句「复制失败」让事情卡死。
  function copyToken(text, btn){
    if (!text) return;
    var done = function(){
      if (btn){ var old = btn.textContent; btn.textContent = '已复制';
        setTimeout(function(){ btn.textContent = old; }, 1500); }
      toast('已复制到剪贴板');
    };
    var fallback = function(){
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.setAttribute('readonly', '');
      // 必须可见地进 DOM 且不能 display:none，iOS 才能选中
      ta.style.position = 'fixed';
      ta.style.top = '-1000px';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      var ok = false;
      try {
        ta.select();
        ta.setSelectionRange(0, text.length);
        ok = document.execCommand('copy');
      } catch (e) {}
      document.body.removeChild(ta);
      if (ok){ done(); return; }
      // 最后兜底：给一个可长按的输入框
      openSheet('手动复制', '自动复制没成功，长按下面的内容全选复制。', function(body){
        var inp = document.createElement('input');
        inp.type = 'text';
        inp.value = text;
        inp.className = 'mono';
        inp.style.width = '100%';
        inp.style.padding = '10px';
        inp.style.borderRadius = '10px';
        inp.style.border = '1px solid var(--line)';
        inp.style.background = 'var(--surface-2)';
        inp.style.color = 'var(--text)';
        body.appendChild(inp);
        inp.focus(); inp.select();
      });
    };
    if (navigator.clipboard && navigator.clipboard.writeText){
      navigator.clipboard.writeText(text).then(done).catch(fallback);
    } else {
      fallback();
    }
  }

  /* ---------- 视图密度：卡片 / 紧凑列表 ----------
     用户反馈「平铺的标签太大占地方」，所以给一个开关：
       card = 现在的完整卡片（能看余额、任务、签到，信息最全）
       list = 一行一个账号（一屏十几个，扫一眼就知道签没签）
     选择持久化，默认仍是卡片（信息更完整，首次使用体验更好）。 */
  var VIEW_KEY = 'points_view';
  var VIEWS = ['card', 'list'];
  function readView(){
    var v = '';
    try { v = localStorage.getItem(VIEW_KEY) || ''; } catch (e) {}
    return VIEWS.indexOf(v) >= 0 ? v : 'card';
  }
  function applyView(force){
    var v = force || readView();
    var btn = $('btnView');
    if (btn){
      // 图标显示「切过去之后会是什么」：当前是卡片就显示列表图标
      btn.innerHTML = ICON(v === 'card' ? 'list' : 'grid');
      btn.setAttribute('aria-label', v === 'card' ? '切换为紧凑显示' : '切换为卡片显示');
      btn.title = v === 'card' ? '紧凑显示' : '卡片显示';
    }
    var list = $('list');
    if (list) list.className = 'list' + (v === 'list' ? ' compact' : '');
    return v;
  }
  function cycleView(){
    var cur = readView();
    var next = (cur === 'card') ? 'list' : 'card';
    try { localStorage.setItem(VIEW_KEY, next); } catch (e) {}
    applyView(next);
    render();
    toast(next === 'list' ? '已切换为紧凑显示' : '已切换为卡片显示');
    return next;
  }

  /* ---------- 拖动排序 ----------
     手机上「长按 → 拖动」是最自然的重排方式。这里不引第三方库，
     自己用 pointer 事件做：
       1) 长按 380ms 进入「排序态」（避免和下拉刷新/点击冲突）
       2) 排序态里跟手移动，用 transform 位移，不触发重排
       3) 松手后按新顺序提交 /api/account/reorder，失败则回滚重渲染
     拖动只在已渲染的卡片之间换位，所以直接操作 DOM 顺序即可。 */
  var dragState = null;
  var DRAG_HOLD = 380;

  function rowKey(node){ return node.getAttribute('data-key'); }

  function beginTrack(node, r){
    node.addEventListener('pointerdown', function(e){
      if (e.button != null && e.button !== 0) return;
      // 按钮/输入框上的按下不触发排序（否则点签到会变成拖动）
      if (e.target.closest('button')) return;
      var startY = e.clientY, startX = e.clientX, timer = null, active = false;
      var lastY = startY;
      function cleanup(){
        clearTimeout(timer);
        document.removeEventListener('pointermove', onMove);
        document.removeEventListener('pointerup', onUp);
        document.removeEventListener('pointercancel', onUp);
      }
      timer = setTimeout(function(){
        active = true;
        node.classList.add('longpressed', 'dragging');
        document.body.classList.add('reordering');
        dragState = { node: node, key: rowKey(node) };
        if (navigator.vibrate) { try { navigator.vibrate(12); } catch (e2) {} }
      }, DRAG_HOLD);
      function onMove(ev){
        var dy = ev.clientY - startY;
        if (!active){
          // 还没到长按时间就移动了：认为用户想滚动，取消长按
          if (Math.abs(dy) > 8 || Math.abs(ev.clientX - startX) > 8){ cleanup(); }
          return;
        }
        ev.preventDefault();
        node.style.transform = 'translateY(' + dy + 'px)';
        // 找应该插入到哪个兄弟节点之前
        var siblings = Array.prototype.slice.call(node.parentNode.children).filter(function(n){
          return n !== node;
        });
        var insertBefore = null;
        for (var i = 0; i < siblings.length; i++){
          var box = siblings[i].getBoundingClientRect();
          if (ev.clientY < box.top + box.height / 2){ insertBefore = siblings[i]; break; }
        }
        siblings.forEach(function(n){ n.classList.remove('drop-slot'); });
        if (insertBefore) insertBefore.classList.add('drop-slot');
        else if (siblings.length) siblings[siblings.length - 1].classList.add('drop-slot');
        node._insertBefore = insertBefore;
        lastY = ev.clientY;
      }
      function onUp(){
        cleanup();
        if (!active) return;
        active = false;
        document.body.classList.remove('reordering');
        var target = node._insertBefore || null;
        node.classList.remove('longpressed', 'dragging');
        node.style.transform = '';
        Array.prototype.forEach.call(node.parentNode.children, function(n){
          n.classList.remove('drop-slot');
        });
        var parent = node.parentNode;
        if (target && target !== node.nextSibling) parent.insertBefore(node, target);
        else if (!target) parent.appendChild(node);
        dragState = null;
        // 顺序变化后提交给后端
        var keys = Array.prototype.slice.call(parent.children)
          .map(function(n){ return n.getAttribute('data-key'); })
          .filter(Boolean)
          .map(function(k){
            var p = k.split('|');
            return { provider: p[0], account_id: p.slice(1).join('|') };
          });
        if (keys.length < 2) { render(); return; }
        var snapshot = parent.innerHTML;
        post('/api/account/reorder', { keys: keys }).then(function(res){
          if (!res || !res.ok){ throw new Error((res && res.error) || '排序失败'); }
          toast('顺序已保存');
          load(true);
        }).catch(function(err){
          toast(err.message || '排序失败');
          // 回滚：把 DOM 还原成拖动前的样子，再拉一次真实数据
          parent.innerHTML = snapshot;
          load(true);
        });
      }
      document.addEventListener('pointermove', onMove, { passive: false });
      document.addEventListener('pointerup', onUp);
      document.addEventListener('pointercancel', onUp);
    });
  }

  /* ---------- 主题：只有浅色 / 深色两种 ---------- */
  // 为什么删掉「跟随系统」：Android WebView 的 prefers-color-scheme 在相当一部分
  // 机型上根本不准（恒 light / 恒 dark），我们绕过媒体查询、从壳层注入
  // Configuration.uiMode、每秒轮询……折腾了三版仍然不可靠。
  // 用户的原话是「你都实现不了，要它有什么用」。所以直接砍掉这个选项：
  // 只有「浅色」「深色」两个确定值，点一下立刻变，没有任何"看系统脸色"的分支。
  // 代价是首次安装不再自动匹配系统外观，换来的是 100% 可预期。
  var THEME_KEY = 'points_theme';
  var THEMES = ['light', 'dark'];
  var THEME_LABEL = { light: '浅色', dark: '深色' };
  var THEME_DEFAULT = 'dark';

  // 读用户选择。历史值 'system' 一律落到默认深色——老用户升级后不会停在
  // 一个已经不存在的模式上，也不会出现「localStorage 是 system 但界面是浅色」的矛盾。
  // 顺手把非法值写回默认，存储里不再留 'system' 这种幽灵值。
  function readTheme(){
    var v = '';
    try { v = localStorage.getItem(THEME_KEY) || ''; } catch (e) {}
    if (THEMES.indexOf(v) >= 0) return v;
    try { localStorage.setItem(THEME_KEY, THEME_DEFAULT); } catch (e) {}
    return THEME_DEFAULT;
  }
  function isDark(){
    return readTheme() === 'dark';
  }
  // 把主题写进 DOM，并同步状态栏颜色给壳层
  function applyTheme(theme){
    var t = THEMES.indexOf(theme) >= 0 ? theme : readTheme();
    var root = document.documentElement;
    root.setAttribute('data-theme', t);
    // color-scheme 决定 WebView 里滚动条/表单控件的明暗
    root.style.colorScheme = t;
    syncStatusBar(t === 'dark');
    var btn = $('btnTheme');
    if (btn){
      btn.innerHTML = ICON(t === 'dark' ? 'moon' : 'sun');
      btn.setAttribute('aria-label', '主题：' + THEME_LABEL[t]);
      btn.title = '主题：' + THEME_LABEL[t];
    }
  }
  // syncStatusBar：状态栏/导航栏要跟页面背景同色，才不像「贴了一条固定色带」。
  // 我们自己画背景（edge-to-edge），原生侧只需要把图标明暗调对。
  function syncStatusBar(dark){
    var bg = dark ? '#141218' : '#FBF8FF';
    var metas = document.querySelectorAll('meta[name="theme-color"]');
    for (var i = 0; i < metas.length; i++){
      metas[i].setAttribute('content', bg);
      metas[i].removeAttribute('media');
    }
    try {
      // Android：壳层读这个变量改系统栏
      window.__pointsTheme = dark ? 'dark' : 'light';
      if (window.PointsBridge && typeof PointsBridge.setTheme === 'function'){
        PointsBridge.setTheme(dark ? 'dark' : 'light');
      }
      // iOS：壳层读这个变量（WKScriptMessageHandler 同名）
      if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.PointsBridge){
        window.webkit.messageHandlers.PointsBridge.postMessage({ action: 'setTheme', theme: dark ? 'dark' : 'light' });
      }
    } catch (e) {}
  }
  // 两态循环：light ⇄ dark
  function cycleTheme(){
    var next = readTheme() === 'dark' ? 'light' : 'dark';
    setTheme(next);
    toast('主题：' + THEME_LABEL[next]);
    return next;
  }
  function setTheme(t){
    if (THEMES.indexOf(t) < 0) t = THEME_DEFAULT;
    try { localStorage.setItem(THEME_KEY, t); } catch (e) {}
    applyTheme(t);
    return t;
  }

  /* ---------- 备份文件读写 ----------
     手机上复制一长串 JSON 再粘贴是最难受的一步：选中容易错、键盘一挡就看不见。
     优先走原生「存到文件 / 从文件选」，两条路：
       1) 壳层桥：Android 走 SAF、iOS 走 UIDocumentPicker（都弹系统文件面板）
       2) 无桥时退回浏览器能力：Blob 下载 / <input type=file>
     文件名带日期，方便用户自己认出哪个是最新的。 */
  function backupName(){
    var d = new Date();
    function p(n){ return (n < 10 ? '0' : '') + n; }
    return '积分矩阵备份-' + d.getFullYear() + p(d.getMonth() + 1) + p(d.getDate()) +
           '-' + p(d.getHours()) + p(d.getMinutes()) + '.json';
  }
  function saveBackupFile(){
    j('/api/export').then(function(d){
      var text = JSON.stringify(d, null, 2);
      var name = backupName();
      // 1) iOS：WKScriptMessageHandler（postMessage 形式，可带参数）
      try {
        if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.PointsBridge){
          window.webkit.messageHandlers.PointsBridge.postMessage(
            { action: 'saveFile', name: name, content: text });
          toast('请选择保存位置');
          return;
        }
      } catch (e) {}
      // 2) Android：addJavascriptInterface 同步方法
      try {
        if (window.PointsBridge && typeof PointsBridge.saveFile === 'function'){
          PointsBridge.saveFile(name, text);
          toast('请选择保存位置');
          return;
        }
      } catch (e) {}
      // 3) 浏览器下载
      try {
        var blob = new Blob([text], { type: 'application/json' });
        var url = URL.createObjectURL(blob);
        var a = document.createElement('a');
        a.href = url; a.download = name;
        document.body.appendChild(a); a.click();
        setTimeout(function(){
          if (a.parentNode) a.parentNode.removeChild(a);
          URL.revokeObjectURL(url);
        }, 800);
        toast('已导出文件');
      } catch (e) {
        toast('导出文件失败，请用「复制到剪贴板」');
      }
    }).catch(function(e){ toast('导出失败：' + e.message); });
  }
  function pickBackupFile(cb){
    // 回调先挂上：两端原生侧都会调用 window.__pointsOnFilePicked(text)
    window.__pointsOnFilePicked = function(text){
      try { delete window.__pointsOnFilePicked; } catch (e) { window.__pointsOnFilePicked = null; }
      if (text) cb(text);
    };
    // 1) iOS
    try {
      if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.PointsBridge){
        window.webkit.messageHandlers.PointsBridge.postMessage({ action: 'pickFile' });
        return;
      }
    } catch (e) {}
    // 2) Android
    try {
      if (window.PointsBridge && typeof PointsBridge.pickFile === 'function'){
        PointsBridge.pickFile();
        return;
      }
    } catch (e) {}
    // 3) 浏览器 <input type=file>
    var inp = document.createElement('input');
    inp.type = 'file';
    inp.accept = '.json,application/json,text/plain';
    inp.style.display = 'none';
    document.body.appendChild(inp);
    inp.addEventListener('change', function(){
      var f = inp.files && inp.files[0];
      if (!f){ if (inp.parentNode) inp.parentNode.removeChild(inp); return; }
      var fr = new FileReader();
      fr.onload = function(){
        if (inp.parentNode) inp.parentNode.removeChild(inp);
        cb(String(fr.result || ''));
      };
      fr.onerror = function(){
        if (inp.parentNode) inp.parentNode.removeChild(inp);
        toast('读取文件失败');
      };
      fr.readAsText(f);
    });
    inp.click();
  }

  /* ---------- 骨架屏 ---------- */
  function showSkeleton(){
    var frag = document.createDocumentFragment();
    for (var i = 0; i < 3; i++){
      var d = document.createElement('div');
      d.className = 'skel';
      frag.appendChild(d);
    }
    var l = $('list'); l.textContent = ''; l.appendChild(frag);
  }

  /* ---------- 渲染 ---------- */
  function renderChips(rows){
    var ok = 0, fail = 0, done = 0;
    for (var i = 0; i < rows.length; i++){
      if (rows[i].error) fail++; else { ok++; if (rows[i].checked_in) done++; }
    }
    var box = $('chips'); box.textContent = '';
    function chip(cls, text){
      var d = document.createElement('span');
      d.className = 'chip ' + cls;
      d.textContent = text;
      box.appendChild(d);
    }
    if (ok) chip('ok', ok + ' 个正常');
    if (done) chip('wait', done + ' 个已签');
    if (fail) chip('bad', fail + ' 个异常');
  }

  // 「任务 / 抽奖」这类入口按钮：样式统一。
  // icon 传空字符串就不渲染 svg —— 任务按钮原来带一个小方框图标，
  // 用户明确说没用还占地方，现在全部只留文字。
  function pillBtn(icon, text, cls, label, onClick){
    var b = document.createElement('button');
    b.className = 'pill ' + cls;
    b.innerHTML = (icon ? ICON(icon) : '') + '<span>' + esc(text) + '</span>';
    if (label) b.setAttribute('aria-label', label);
    b.addEventListener('click', function(e){
      if (e) e.stopPropagation();
      onClick(e);
    });
    return b;
  }

  // 账号卡片（完整视图）
  function cardOf(r){
    var card = document.createElement('div');
    card.className = 'card' + (r.error ? ' err' : '');
    card.setAttribute('data-key', (r.provider || '') + '|' + (r.account_id || ''));

    var top = document.createElement('div'); top.className = 'c-top';
    var av = document.createElement('div'); av.className = 'avatar';
    var hue = hueOf(r.account_id || r.provider);
    av.style.background = 'linear-gradient(135deg,hsl(' + hue + ',72%,58%),hsl(' + ((hue + 34) % 360) + ',72%,50%))';
    av.textContent = initials(r.provider_name || r.provider || r.label || r.account_id);
    var nm = document.createElement('div'); nm.className = 'c-name';
    var p = document.createElement('div'); p.className = 'p';
    p.textContent = r.provider_name || r.provider || '';
    var s = document.createElement('div'); s.className = 's';
    s.textContent = r.label || r.account_id || '';
    nm.appendChild(p); nm.appendChild(s);
    top.appendChild(av); top.appendChild(nm);

    // 余额和标题同一行、靠右。
    // 0.6.4：右上角那个「···」已彻底删除（用户原话「这个玩意不要给我保留了」），
    // 余额成为标题行最右侧的元素，卡片右侧不再多一列 32px 的空按钮位。
    var bal = document.createElement('div'); bal.className = 'balance';
    var n = document.createElement('span'); n.className = 'n';
    n.textContent = r.error ? '—' : fmtMoney(r.balance);
    var u = document.createElement('span'); u.className = 'u'; u.textContent = r.unit || '积分';
    bal.appendChild(n); bal.appendChild(u);
    top.appendChild(bal);
    card.appendChild(top);

    var foot = document.createElement('div'); foot.className = 'c-foot';
    var st = document.createElement('div');
    st.className = 'status ' + (r.error ? 'bad' : (r.checked_in ? 'ok' : ''));
    var stTxt = document.createElement('span'); stTxt.className = 'txt';
    stTxt.textContent = detailText(r);
    st.appendChild(stTxt);

    // 右侧按钮组：任务 / 抽奖 直接摆出来，只有「API 令牌」收进 ··· 菜单。
    //
    // 0.6.1 修正：0.6.0 时把三个都收进菜单，理解错了用户的意思——
    // 他说「收进 ··· 菜单，因为不常用」指的是令牌，任务和抽奖仍是高频入口，
    // 藏进菜单等于每次多点两下。
    var btns = document.createElement('div');
    btns.className = 'c-actions';

    if (r.has_tasks && !r.error){
      btns.appendChild(pillBtn('', '任务', 'task',
        '查看 ' + (r.provider_name || r.provider || '') + ' 每日任务',
        function(){ openTasks(r); }));
    }
    // 抽奖入口不判断 r.error：余额查询失败（比如刚过期）不代表抽奖也失败，
    // 让用户点进去看真实状态，比事先替他判定更有用。
    if (r.has_lottery){
      btns.appendChild(pillBtn('', '抽奖', 'lottery',
        '查看 ' + (r.provider_name || r.provider || '') + ' 抽奖',
        function(){ openLottery(r); }));
    }

    var btn = document.createElement('button');
    btn.className = 'pill' + (r.error ? ' fail' : (r.checked_in ? ' done' : ''));
    btn.setAttribute('aria-label', (r.error ? '重试 ' : (r.checked_in ? '已签到 ' : '签到 ')) + (r.provider_name || r.provider || ''));
    btn.textContent = r.error ? '重试' : (r.checked_in ? '已签' : '签到');
    if (r.checked_in) btn.disabled = true;
    btn.addEventListener('click', function(){
      doCheckin(r.account_id, btn);
    });
    btns.appendChild(btn);

    foot.appendChild(st); foot.appendChild(btns);
    card.appendChild(foot);
    return card;
  }

  // 账号行（紧凑视图）：一行之内放完头像 / 名称 / 余额 / 操作
  //
  // 0.6.4：这里原本还有个「···」令牌入口，现已删除。账号管理与令牌统一在
  // 设置 → 账号 → 点某个账号 里，主列表只保留「任务 / 抽奖 / 签到」三个动作。
  function rowOf(r){
    var row = document.createElement('div');
    row.className = 'row-item' + (r.error ? ' err' : '');
    row.setAttribute('data-key', (r.provider || '') + '|' + (r.account_id || ''));

    var av = document.createElement('div'); av.className = 'avatar';
    var hue = hueOf(r.account_id || r.provider);
    av.style.background = 'linear-gradient(135deg,hsl(' + hue + ',72%,58%),hsl(' + ((hue + 34) % 360) + ',72%,50%))';
    av.textContent = initials(r.provider_name || r.provider || r.label || r.account_id);
    row.appendChild(av);

    var main = document.createElement('div'); main.className = 'row-main';
    var t = document.createElement('div'); t.className = 't';
    // 平台名和备注相同时只写一遍。之前固定拼「平台 · 备注」，
    // 备注默认就等于平台名（比如「AiPy Pro · AiPy Pro」），
    // 白白吃掉一半宽度，把名字挤成省略号——这就是「紧凑显示不全」的主因。
    var pfName = r.provider_name || r.provider || '';
    var lbl = r.label || r.account_id || '';
    t.textContent = (lbl && lbl !== pfName) ? pfName + ' · ' + lbl : pfName;
    var d = document.createElement('div'); d.className = 'd' + (r.error ? ' bad' : '');
    // 余额并入副标题：「1,200 积分 · 今日已签」。
    //
    // 之前余额是独立一列、夹在名字和按钮之间——而每行的按钮数量不一样
    // （有任务/抽奖/···/签到之分），按钮组一宽就把余额推得左右乱跑，
    // 三行看下来余额根本不在一条竖线上。挪进左列之后：
    // 左边两行文字统一从头像右侧起，右边按钮统一右对齐，两个基准都稳了。
    if (!r.error){
      var bn = document.createElement('b'); bn.className = 'd-bal';
      bn.textContent = fmtMoney(r.balance);
      d.appendChild(bn);
      d.appendChild(document.createTextNode(' ' + (r.unit || '积分')));
      var sep = document.createElement('span'); sep.className = 'd-sep';
      sep.textContent = ' · ' + detailText(r);
      d.appendChild(sep);
    } else {
      d.textContent = detailText(r);
    }
    main.appendChild(t); main.appendChild(d);
    row.appendChild(main);

    var act = document.createElement('div'); act.className = 'row-act';
    // 紧凑行：任务 / 抽奖 用文字小胶囊（高频），其余动作一个都不放。
    //
    // 0.6.2：上一版这里放的是 30px 的纯图标方框，两个图标吃掉 64px 宽度，
    // 名字被挤成「AiPy Pro · AiPy ...」，而且图标本身也看不出是任务还是抽奖。
    // 换成同样尺寸的文字反而更窄、更好认。
    // 0.6.4：原本夹在「任务」和「签到」之间的 ··· 已删除（用户：「三个点占地方」）。
    if (r.has_tasks && !r.error){
      act.appendChild(pillBtn('', '任务', 'mute', '任务', function(){ openTasks(r); }));
    }
    if (r.has_lottery){
      act.appendChild(pillBtn('', '抽奖', 'mute', '抽奖', function(){ openLottery(r); }));
    }
    var btn = document.createElement('button');
    btn.className = 'pill' + (r.error ? ' fail' : (r.checked_in ? ' done' : ''));
    btn.textContent = r.error ? '重试' : (r.checked_in ? '已签' : '签到');
    btn.setAttribute('aria-label', (r.error ? '重试 ' : (r.checked_in ? '已签到 ' : '签到 ')) + (r.provider_name || r.provider || ''));
    if (r.checked_in) btn.disabled = true;
    btn.addEventListener('click', function(){ doCheckin(r.account_id, btn); });
    act.appendChild(btn);
    row.appendChild(act);
    return row;
  }

  function render(){
    var rows = state.rows;
    var list = $('list');
    var total = 0, healthy = 0, unit = '';
    for (var i = 0; i < rows.length; i++){
      if (!rows[i].error){
        total += Number(rows[i].balance) || 0;
        healthy++;
        // 各家单位不同（积分 / 灵石 / 智点）时不能相加。
        // 注意：unit 缺省即「积分」（provider 约定空字符串 = 积分），
        // 所以这里必须先归一化再比较，否则 WorkBuddy 的 undefined 会被当成
        // 一个「新单位」，反而把本来一致的合计判成不可合计。
        var u = rows[i].unit || '积分';
        if (unit && unit !== u) unit = '合计'; else unit = u;
      }
    }
    // 全异常或没有账号时显示「—」而不是 0：0 会被误读成「余额为零」。
    var mixed = (unit === '合计');
    var tn = $('total');
    // 混合单位时整块合计隐藏：这个数没有意义，说明文字也已按用户要求删掉。
    var showDash = (!rows.length || !healthy || mixed);
    // 没有总数时把「全部账号合计 + 数值 + 单位」整块隐藏。
    // 单藏数字会留下一个孤零零的「全部账号合计」标题，更费解。
    var head = $('hero-head');
    if (head) head.className = 'hero-head' + (showDash ? ' off' : '');
    var row = $('total-row');
    if (row) row.className = 'total';
    if (!showDash){
      tn.textContent = fmt(total);
      tn.className = 'num';
    }
    var tu = $('total-unit');
    if (tu) tu.textContent = showDash ? '' : (unit || '积分');
    // 没有总数时不写任何解释文案：用户反馈「单位不同所以不合计」那段
    // 既占地方又没用——数字本来就在下面每张卡上，一眼就能看到。
    // 单位混在一起时整块合计区直接隐藏，把空间还给账号列表。
    renderChips(rows);

    if (!rows.length){
      list.innerHTML =
        '<div class="empty">' +
          '<div class="ring">' + ICON('spark') + '</div>' +
          '<h2>还没有账号</h2>' +
          '<p>点下面的「添加账号」，用官方网页登录一次。\n之后每次打开本应用会自动签到。</p>' +
        '</div>';
      $('btnAll').disabled = true;
      $('btnAll').setAttribute('aria-disabled', 'true');
      return;
    }

    // 一次插入，避免逐条 append 引起多次重排
    var view = applyView();
    var frag = document.createDocumentFragment();
    for (var k = 0; k < rows.length; k++){
      frag.appendChild(view === 'list' ? rowOf(rows[k]) : cardOf(rows[k]));
    }
    list.textContent = '';
    list.appendChild(frag);
    // 挂上长按拖动（在插入之后才能拿到真实节点）
    for (var m = 0; m < list.children.length; m++){
      beginTrack(list.children[m]);
    }
    $('btnAll').disabled = state.busy;
  }

  /* ---------- 数据 ---------- */
  function enrich(rows){
    var byId = {};
    for (var i = 0; i < state.providers.length; i++) byId[state.providers[i].id] = state.providers[i];
    for (var j = 0; j < rows.length; j++){
      var p = byId[rows[j].provider];
      if (p) {
        rows[j].provider_name = p.name;
        rows[j].has_tasks = !!p.tasks;
        rows[j].has_lottery = !!p.lottery;
        rows[j].has_tokens = !!p.tokens;
      }
    }
    return rows;
  }

  // hasTokens：这个账号所属平台有没有令牌管理能力。
  //
  // 数据来源是 /api/providers 的 Meta.Tokens（目前只有 AiPy）。
  // 为什么不直接读 r.has_tokens：设置页账号列表来自 /api/accounts，
  // 那份数据不含能力字段，只有列表行（enrich 打过标记）才有。
  function hasTokens(r){
    if (r && r.has_tokens) return true;
    for (var i = 0; i < state.providers.length; i++){
      if (state.providers[i].id === (r && r.provider)) return !!state.providers[i].tokens;
    }
    return false;
  }

  function load(silent){
    if (!silent) showSkeleton();
    return j('/api/providers').then(function(ps){
      state.providers = Array.isArray(ps) ? ps : [];
      // allSettled 而不是 all（0.6.4）。
      //
      // 原来用 Promise.all：查询或者账号列表任一失败，整轮 load 直接判失败，
      // 界面被替换成「读取失败」，连上一次已经拿到的数据都看不见了。
      // 但实际上这两路是独立的——账号列表是从本地文件读的，几乎不会失败，
      // 真失败的是查询。用 allSettled 之后：哪路成功就用哪路的数据，
      // 失败的那路保留上一次的值，界面不打回空白。
      return new Promise(function(resolve){
        if (typeof Promise.allSettled !== 'function'){
          // 老 WebView 兜底：等价语义手动实现
          var got = [null, null], left = 2;
          function done(){ if (--left === 0) resolve(got); }
          j('/api/query').then(function(v){ got[0] = {status:'fulfilled', value:v}; done(); },
                               function(e){ got[0] = {status:'rejected', reason:e}; done(); });
          j('/api/accounts').then(function(v){ got[1] = {status:'fulfilled', value:v}; done(); },
                                  function(e){ got[1] = {status:'rejected', reason:e}; done(); });
          return;
        }
        Promise.allSettled([j('/api/query'), j('/api/accounts')]).then(resolve);
      });
    }).then(function(both){
      var q = both[0] || {}, a = both[1] || {};
      var rows = (q.status === 'fulfilled' && Array.isArray(q.value)) ? q.value : null;
      var accts = (a.status === 'fulfilled' && Array.isArray(a.value)) ? a.value : null;
      if (rows){
        if (accts){
          var enabled = {};
          for (var i = 0; i < accts.length; i++) enabled[accts[i].account_id] = accts[i].enabled;
          for (var k = 0; k < rows.length; k++){
            if (enabled.hasOwnProperty(rows[k].account_id)) rows[k].enabled = enabled[rows[k].account_id];
          }
        }
        state.rows = enrich(rows);
        render();
        return true;
      }
      // 查询失败：保留旧数据，只在活动条上提示一次，绝不把列表打回空白。
      if (state.rows.length){
        actErr('读取失败：' + ((q.reason && q.reason.message) || '请稍后重试'), 6000);
        return false;
      }
      var list = $('list');
      list.innerHTML =
        '<div class="empty"><div class="ring">' + ICON('warn') + '</div>' +
        '<h2>读取失败</h2><p>' + esc((q.reason && q.reason.message) || '未知错误') + '</p>' +
        '<button class="btn filled" id="retryBtn" style="max-width:220px">重新加载</button></div>';
      var rb = $('retryBtn');
      if (rb) rb.addEventListener('click', function(){ load(false); });
      return false;
    }).catch(function(e){
      var list = $('list');
      list.innerHTML =
        '<div class="empty"><div class="ring">' + ICON('warn') + '</div>' +
        '<h2>读取失败</h2><p>' + esc(e.message) + '</p>' +
        '<button class="btn filled" id="retryBtn" style="max-width:220px">重新加载</button></div>';
      var rb = $('retryBtn');
      if (rb) rb.addEventListener('click', function(){ load(false); });
      return false;
    });
  }

  // 状态行的统一文案：能拿到数值才写「+N 单位」，拿不到就只写状态。
  //
  // 历史：先是不带数字（用户投诉「今日已签后面没有 +N」），后来为了显数字
  // 又搞了个「+?」占位（用户说「很搞笑」）。教训是——宁可不显示，也不要显示
  // 一个看起来像出错的东西。所以现在：
  //   有 today_gain → 今日已签 +N 单位
  //   没有          → 今日已签          （干净收尾，不留悬念）
  // 小数点位：灵石这类小额单位，0.04 会被 fmt 四舍五入成「0」，
  // 显示「+0 灵石」「0.1 灵石」看着像坏了。这里按数量级自适应：
  //   >= 100 → 整数；>= 1 → 一位小数；否则两位小数。
  function fmtGain(v){
    var n = Number(v);
    if (!isFinite(n) || n <= 0) return '';
    if (n >= 100) return fmt(n);
    if (n >= 1) return (Math.round(n * 10) / 10).toString();
    return (Math.round(n * 100) / 100).toString();
  }
  function gainNum(r){
    var v = Number(r && r.today_gain);
    return isFinite(v) && v > 0 ? fmtGain(v) : '';
  }
  function detailText(r){
    if (!r) return '';
    var unit = r.unit || '积分';
    if (r.error) return r.error;
    if (r.checked_in){
      var g = gainNum(r);
      return g ? ('今日已签 +' + g + ' ' + unit) : '今日已签';
    }
    // 未签到但平台给了今日可得积分时，也顺手告诉用户能拿多少
    var g2 = gainNum(r);
    if (g2) return '今日未签 · 可得 +' + g2 + ' ' + unit;
    return r.detail || '今日未签';
  }

  // 统一的「，+N 单位」文案。所有奖励提示都必须走这里——
  // 之前各调用点各写各的，漏了单位就变成光秃秃的「+100」，用户看不出是积分还是次数。
  function gainTxt(r){
    if (!r || !r.gain) return '';
    return '，+' + fmtGain(r.gain) + ' ' + (r.unit || '积分');
  }

  // 已签但平台没回具体积分数时的兜底说明。
  // 用户反馈「早上就看到已签到，是不是有时间限制」——顺带解释一句，
  // 免得他以为是软件判断错了。
  function alreadyWhy(r){
    return (r && r.provider === 'dumate') ? '（搭子当天打开网页即自动发放）' : '';
  }

  /* ---------- 活动条：把后台动作暴露给用户 ---------- */
  // 为什么要它：用户完全看不出「自动签到」到底有没有在跑——打开 App 看到
  // 未签，就以为要自己点；点了又不知道是不是重复。把每一步写出来，
  // 「谁在签、签到第几个、签完没、刷新没」一目了然。
  var actTimer = null;
  function actShow(text){
    var a = $('act'), t = $('actTxt');
    if (!a || !t) return;
    clearTimeout(actTimer);
    a.className = 'act on';
    t.textContent = text;
  }
  function actDone(text, holdMs){
    var a = $('act'), t = $('actTxt');
    if (!a || !t) return;
    clearTimeout(actTimer);
    a.className = 'act on done';
    t.textContent = text;
    actTimer = setTimeout(function(){ a.className = 'act'; }, holdMs || 3000);
  }
  function actErr(text, holdMs){
    var a = $('act'), t = $('actTxt');
    if (!a || !t) return;
    clearTimeout(actTimer);
    a.className = 'act on err';
    t.textContent = text;
    actTimer = setTimeout(function(){ a.className = 'act'; }, holdMs || 5000);
  }
  function actHide(){ var a = $('act'); clearTimeout(actTimer); if (a) a.className = 'act'; }
  function pname(r){
    return (r && (r.provider_name || r.provider)) || '账号';
  }
  function pshort(r){
    return (r && (r.label || r.account_id)) ? (' · ' + (r.label || r.account_id)) : '';
  }

  function doCheckin(id, btn){
    // 手动点单个账号的「签到」也要有过程反馈：按钮变「…」只是局部变化，
    // 顶部活动条才是用户真正在看的地方。
    var row = null;
    for (var k = 0; k < state.rows.length; k++){
      if (state.rows[k].account_id === id){ row = state.rows[k]; break; }
    }
    if (btn){ btn.disabled = true; btn.textContent = '…'; }
    actShow((row ? pname(row) + pshort(row) : '账号') + ' 正在签到…');
    return j('/api/checkin?account=' + encodeURIComponent(id), { method: 'POST' })
      .then(function(res){
        var r = Array.isArray(res) ? res[0] : res;
        if (r && r.error){
          actErr('签到失败：' + r.error, 5000);
          toast(r.error);
        }
        else if (r && r.already){
          actDone(pname(row) + ' 今日已签' + gainTxt(r), 2500);
          toast('今日已签' + gainTxt(r) + (r.gain ? '' : alreadyWhy(r)));
        }
        else if (r && r.ok){
          actDone(pname(row) + ' 签到成功' + gainTxt(r), 2500);
          toast('签到成功' + gainTxt(r));
        }
        else {
          actErr('签到未成功，可下拉刷新重试', 5000);
          toast('签到未成功');
        }
        actShow('正在刷新列表…');
        return load(true);
      })
      .catch(function(e){
        actErr('签到失败：' + e.message, 5000);
        toast('签到失败：' + e.message);
        if (btn) btn.disabled = false;
      });
  }

  // 一键做完所有事：逐个账号签到 → 领 AiPy 任务奖励 → 百度搭子抽奖。
  //
  // 关键改动（0.5.4）：签到从「一次 POST 让后端批量串行」改成「前端逐个 POST」。
  // 原因：批量接口只有一个最终响应，中途什么都不知道——用户打开 App 看到
  // 「今日未签」，几秒后又变「今日已签」，中间发生了什么完全空白，于是怀疑
  // 「是不是要我自己点」「是不是后台签了前台没刷新」。改成逐个请求后，
  // 每签一个就更新顶部活动条：百度搭子 正在签到…（2/3）→ 已签 +500 积分。
  //
  // 关键改动（0.5.5）：打开 App 走的就是这一个函数，和点「一键签到」完全同一条
  // 路径——签到 + 领任务奖励 + 抽奖三件套一起做。之前启动路径只签到、不领奖，
  // 于是同一份代码跑出两种结果，用户看到「开 App 没领奖，点按钮才领」。
  //
  // 并发策略仍是串行（一个签完再发下一个），保留原来的防风控意图；
  // 账号之间留 250ms 间隔，不制造瞬时并发。
  function checkinAll(opts){
    if (state.busy) return Promise.resolve(null);
    var quiet = !!(opts && opts.quiet);   // 启动自动跑：完成后不弹 toast，只更新活动条
    // 轮次 id：本次执行的所有日志共用它，用户贴日志时能顺出完整时间线
    var round = roundId();
    state.busy = true;
    var b = $('btnAll');
    var origHTML = b ? b.innerHTML : '';
    if (b){ b.disabled = true; b.textContent = '签到中…'; }
    var stat = { ok: 0, skip: 0, bad: 0, taskClaimed: 0, taskGain: 0, taskUnit: '',
                 drawn: 0, drawGain: 0, drawPrizes: [], drawUnit: '',
                 // lines 是逐账号的结果明细，只为「这一次推送」收集。
                 // 不能回头从 state.rows 反推：那一轮成功几个、失败原因是什么，
                 // 只有在请求返回的那一刻才知道。
                 lines: [] };

    // 待签队列必须在「先读一遍状态」之后才组装。
    //
    // 0.5.5 修复：之前是在函数一进来就用 state.rows 组装 todo，
    // 而打开 App 的那一刻 state.rows 还是空的（列表还没查过），
    // 结果 todo 永远是 0 个 —— 于是启动时「一个都不签」，只跑领奖和刷新，
    // 用户看到的就是「打开 App 不签到，点按钮才签」。
    // 现在先 load() 拿到真实状态，再挑未签的账号逐个签。
    var todo = [];
    var idx = 0;
    var total = 0;

    function loadStates(){
      return load(true).then(function(){
        todo = [];
        for (var i = 0; i < state.rows.length; i++){
          var r = state.rows[i];
          if (!r.error && !r.checked_in) todo.push(r);
        }
        total = todo.length;
        return true;
      });
    }

    function step(){
      if (idx >= total) return Promise.resolve();
      var row = todo[idx];
      var n = idx + 1;
      actShow(pname(row) + pshort(row) + ' 正在签到…（' + n + '/' + total + '）');
      return j('/api/checkin?account=' + encodeURIComponent(row.account_id), { method: 'POST' })
        .then(function(res){
          var x = Array.isArray(res) ? res[0] : res;
          if (x && x.error){
            stat.bad++;
            stat.lines.push({ provider: row.provider, label: pname(row) + pshort(row),
              status: 'fail', message: x.error });
            markDaily('account', round, row, 'fail');
            actErr(pname(row) + ' 签到失败：' + x.error, 4000);
          } else if (x && x.already){
            stat.skip++;
            stat.lines.push({ provider: row.provider, label: pname(row) + pshort(row),
              status: 'already', message: '今日已签', gain: (x && x.gain) || 0,
              unit: (x && x.unit) || '' });
            markDaily('account', round, row, 'already');
            actDone(pname(row) + ' 今日已签' + gainTxt(x), 1600);
          } else if (x && x.ok){
            stat.ok++;
            stat.lines.push({ provider: row.provider, label: pname(row) + pshort(row),
              status: 'ok', message: '签到成功', gain: (x && x.gain) || 0,
              unit: (x && x.unit) || '' });
            markDaily('account', round, row, 'ok');
            actDone(pname(row) + ' 签到成功' + gainTxt(x), 1600);
          } else {
            stat.bad++;
            stat.lines.push({ provider: row.provider, label: pname(row) + pshort(row),
              status: 'fail', message: (x && x.message) || '签到未成功' });
            markDaily('account', round, row, 'fail');
            actErr(pname(row) + ' 签到未成功', 4000);
          }
        }, function(e){
          stat.bad++;
          stat.lines.push({ provider: row.provider, label: pname(row) + pshort(row),
            status: 'fail', message: (e && e.message) || '网络错误' });
          markDaily('account', round, row, 'fail');
          actErr(pname(row) + ' 签到失败：' + (e && e.message ? e.message : '网络错误'), 4000);
        })
        .then(function(){
          idx++;
          // 账号之间留一点间隔：上一家的风控是按「同设备短时间连续写」判的
          return new Promise(function(res){ setTimeout(res, 250); }).then(step);
        });
    }

    // 顺序：读状态 → 逐个签到 → 领任务奖励 → 抽奖 → 刷新列表
    actShow('正在获取账号状态…');
    // 打点「今天开始跑一轮」：落盘，进程被杀也不会因为「又开了一次 App」重签
    markDaily('begin', round, quiet ? 'start' : 'manual');
    return loadStates()
      .then(function(){
        actShow(total ? ('准备签到，共 ' + total + ' 个待签账号…') : '账号都已是已签状态，检查奖励…');
        return step();
      })
      .then(function(){
        // 签完立刻领 AiPy 任务奖励（签到会解锁部分任务）
        if (b) b.textContent = '领奖励…';
        actShow('正在领取任务奖励…');
        return post('/api/tasks/claim_all', {});
      })
      .then(function(res){
        var rows = (res && res.results) || [];
        for (var i = 0; i < rows.length; i++){
          stat.taskClaimed += rows[i].claimed || 0;
          stat.taskGain += rows[i].gain || 0;
          if (rows[i].unit) stat.taskUnit = rows[i].unit;
        }
        // 再抽百度搭子的奖（每次签到送一次机会）
        if (b) b.textContent = '抽奖中…';
        actShow('正在抽奖…');
        return post('/api/lottery/draw_all', {});
      })
      .then(function(res){
        var rows = (res && res.results) || [];
        for (var i = 0; i < rows.length; i++){
          stat.drawn += rows[i].drawn || 0;
          stat.drawGain += rows[i].gain || 0;
          if (rows[i].unit) stat.drawUnit = rows[i].unit;
          var pz = rows[i].prizes || [];
          for (var k = 0; k < pz.length; k++){ if (pz[k]) stat.drawPrizes.push(pz[k]); }
        }
        actShow('正在刷新列表…');
        return load(true).then(function(){
          // 本地回环刷新通常 <100ms，这一步一闪而过，用户会以为「跳过了」。
          // 留 400ms 让它确实可见——「签完还要刷新」本身就是需要被看见的信息。
          return new Promise(function(res){ setTimeout(res, 400); });
        });
      })
      .then(function(){
        var sum = summaryOf(stat);
        // 走完一轮才落「今天已跑过」：中途被杀会停在 begin 的 Done=false，
        // 下次打开允许补跑，不会被误判成「今天签完了」
        markDaily('finish', round);
        // 一次一键签到只推一条汇总（用户明确要求，不逐账号刷屏）。
        // 不 await：推送是附加价值，绝不该让「完成」提示等它。
        pushSummary(quiet ? 'start' : 'manual', stat, sum);
        if (!quiet) toast(sum);
        // 收尾文案：让用户明确知道「这轮结束了，页面已是最新」
        if (stat.bad){
          actErr('完成 · ' + sum + '，下拉可重试', 6000);
        } else {
          actDone('完成 · ' + sum, 4000);
        }
      }, function(e){
        actErr('签到失败：' + (e && e.message ? e.message : '网络错误'), 6000);
        if (!quiet) toast('签到失败：' + e.message);
      })
      .then(function(){
        state.busy = false;
        if (b){ b.disabled = false; b.innerHTML = origHTML; }
        return stat;
      });
  }

  // pushSummary：把这一轮的结果交给 Go 侧推送。
  //
  // 三条规则，全部来自用户要求与实际踩坑：
  //   1. 一次一键签到只推一条汇总，不逐账号发；
  //   2. 0.6.7 起「什么时候推」开关全删：启用通知后每轮都推，前端只如实上报；
  //   3. 推送本身失败绝不弹错、绝不影响签到结果，只在控制台留一行。
  // 只要这一轮确实扫过账号就上报，空列表才跳过。
  function pushSummary(trigger, stat, sum){
    // 关键修复（0.6.1）：以前 lines 只在「逐个签到」的循环里填充，
    // 而循环只遍历未签账号。账号全部已签时 todo 为空 → lines 为空 →
    // 这里直接 return，请求根本没发出去。
    //
    // 表现就是用户说的「只有发测试能收到，开关全开也收不到」：
    // 他日常打开 App 时账号早已签完，这一轮没有任何「签到动作」，
    // 于是永远走不到推送。而发测试是 force=true 绕过了所有过滤。
    //
    // 现在补齐兜底：这轮确实扫过账号（state.rows 非空）但没有产生签到明细时，
    // 按 rows 组装一份「全部已签」的明细照样上报，交给后端推送。
    var lines = (stat && stat.lines) || [];
    if (!lines.length){
      var fallback = [];
      for (var i = 0; i < state.rows.length; i++){
        var r = state.rows[i];
        if (r.error) continue;
        fallback.push({
          provider: r.provider, label: pname(r) + pshort(r),
          status: r.checked_in ? 'already' : 'ok',
          message: r.checked_in ? '今日已签' : '本轮无需处理',
          gain: 0, unit: ''
        });
      }
      if (!fallback.length) return;
      lines = fallback;
    }
    post('/api/notify', {
      trigger: trigger || 'manual',
      accounts: lines,
      summary: sum,
      // 本机相对 UTC 的偏移分钟数（东八区是 -480）。Go 在手机上取不到系统时区，
      // 不带这个字段推出去的时间会是 UTC，用户看到的「7:26 签到」变成「23:26」。
      tz_offset: new Date().getTimezoneOffset()
    }).then(function(r){
      if (r && r.error && !r.skipped) console.warn('通知推送失败：' + r.error);
    }).catch(function(e){
      console.warn('通知推送失败：' + (e && e.message));
    });
  }

  // 汇总里的「 +N 单位」片段。必须带单位，且数字和单位之间留空格——
  // 之前写成 '+100' + '积分' 挤成「+100积分」，读起来别扭。
  function gainPart(v, unit){
    if (!v) return '';
    return ' +' + fmtGain(v) + ' ' + (unit || '积分');
  }

  // 把三步结果压成一句话：信息密度高，但不能长到被截断。
  function summaryOf(s){
    var parts = ['新签 ' + s.ok];
    if (s.skip) parts.push('已签 ' + s.skip);
    if (s.bad) parts.push('失败 ' + s.bad);
    var head = parts.join('，');
    var extra = [];
    if (s.taskClaimed > 0){
      extra.push('任务 ' + s.taskClaimed + ' 个' + gainPart(s.taskGain, s.taskUnit));
    }
    if (s.drawn > 0){
      extra.push('抽奖 ' + s.drawn + ' 次' + gainPart(s.drawGain, s.drawUnit));
    }
    if (!extra.length) return head;
    return head + '｜' + extra.filter(Boolean).join('，');
  }

  /* ---------- 下拉刷新 ---------- */
  (function pullToRefresh(){
    var puller = $('puller'), startY = 0, pulling = false, dist = 0;
    var THRESHOLD = 68;
    document.addEventListener('touchstart', function(e){
      if (window.scrollY > 2 || state.busy) return;
      startY = e.touches[0].clientY; pulling = true; dist = 0;
    }, { passive: true });
    document.addEventListener('touchmove', function(e){
      if (!pulling) return;
      var d = e.touches[0].clientY - startY;
      if (d <= 0){ pulling = false; return; }
      if (window.scrollY > 2){ pulling = false; return; }
      // 阻尼：越拉越沉，手感接近原生
      dist = Math.min(112, d * 0.52);
      puller.classList.add('on');
      puller.style.transform = 'translate(-50%,' + (dist - 70) + 'px)';
      puller.classList.toggle('ready', dist >= THRESHOLD * 0.52);
    }, { passive: true });
    document.addEventListener('touchend', function(){
      if (!pulling) return;
      pulling = false;
      puller.classList.remove('on', 'ready');
      puller.style.transform = '';
      if (dist >= THRESHOLD * 0.52){
        puller.classList.add('busy');
        load(true).then(function(){
          puller.classList.remove('busy');
          toast('已刷新');
        });
      }
    });
  })();

  /* ---------- 滚动时给 AppBar 加分隔线 ---------- */
  window.addEventListener('scroll', function(){
    $('appbar').classList.toggle('scrolled', window.scrollY > 4);
  }, { passive: true });

  /* ---------- 事件绑定 ---------- */
  $('btnRefresh').addEventListener('click', function(){
    var b = this; b.classList.add('spin');
    load(true).then(function(){ b.classList.remove('spin'); });
    setTimeout(function(){ b.classList.remove('spin'); }, 1200);
  });
  $('btnAll').addEventListener('click', checkinAll);
  $('btnTheme').addEventListener('click', cycleTheme);
  var vb = $('btnView');
  if (vb) vb.addEventListener('click', cycleView);
  var hb = $('btnHelp');
  if (hb) hb.addEventListener('click', openHelp);
  $('btnAdd').addEventListener('click', openAddSheet);
  $('btnSettings').addEventListener('click', openSettings);

  /* ---------- 添加账号：底部弹层选平台 ---------- */
  /* ---------- 添加账号：选平台 → 选方式 ---------- */
  var BRAND = {
    workbuddy: ['#5B6CFF', '#7B5CFF'],
    aipy: ['#FF7A3D', '#FF5A5F'],
    dumate: ['#2F6BFF', '#4C8DFF'],
    lingxi: ['#12B886', '#0CA678'],
    trae: ['#FF4D6D', '#E63E8B']
  };

  // 平台色块 + 首字，不复制官方 logo（避免商标问题），颜色能一眼分辨。
  function platformRow(p){
    var row = el('button', 'sheet-item');
    var dot = el('span', 'sheet-dot');
    var b = BRAND[p.id];
    if (b){
      dot.style.background = 'linear-gradient(135deg,' + b[0] + ',' + b[1] + ')';
    } else {
      var hue = hueOf(p.id);
      dot.style.background = 'linear-gradient(135deg,hsl(' + hue + ',72%,58%),hsl(' + ((hue + 34) % 360) + ',72%,50%))';
    }
    dot.textContent = initials(p.name || p.id);
    var txt = el('span', 'sheet-txt');
    var t1 = el('b', '', p.name || p.id);
    var t2 = el('i', '', p.note || '');
    txt.appendChild(t1); txt.appendChild(t2);
    var ar = el('span', 'sheet-arw');
    ar.innerHTML = ICON('arrow');
    row.appendChild(dot); row.appendChild(txt); row.appendChild(ar);
    return row;
  }

  function openAddSheet(){
    // 底部不再有手动粘贴兜底按钮，也不显示关闭按钮——用户说这俩多余。
    // 手动粘贴在选平台后的二级页里仍有；备份导入在「设置 → 数据」里也有。
    // 点遮罩仍可关闭本弹层，点平台进入下一级也会自动关闭。
    openSheet('添加账号', '选平台后可以用网页登录，也可以直接粘贴凭据。', function(body, close){
      var ps = state.providers.length ? state.providers : [];
      if (!ps.length){
        body.appendChild(el('div', 'sheet-sub', '平台列表加载中，请稍后重试。'));
      }
      ps.forEach(function(p){
        var row = platformRow(p);
        row.addEventListener('click', function(){
          close();
          setTimeout(function(){ openAddMethod(p); }, 260);
        });
        body.appendChild(row);
      });
    }, { hideCancel: true });
  }

  // 二级：同一个平台，两种添加方式
  function openAddMethod(p){
    openSheet(p.name || p.id, '网页登录偶尔会因平台改版抓不到会话；手动粘贴最稳妥。', function(body, close){
      var web = el('button', 'sheet-item');
      web.appendChild(el('span', 'sheet-txt', ''));
      var wt = web.firstChild;
      wt.appendChild(el('b', '', '网页登录'));
      wt.appendChild(el('i', '', '在内置浏览器打开官网，登录完点「完成」'));
      var war = el('span', 'sheet-arw'); war.innerHTML = ICON('arrow');
      web.appendChild(war);
      web.addEventListener('click', function(){
        close();
        setTimeout(function(){ startLogin(p.id); }, 260);
      });
      body.appendChild(web);

      var hand = el('button', 'sheet-item');
      var ht = el('span', 'sheet-txt');
      ht.appendChild(el('b', '', '手动粘贴凭据'));
      ht.appendChild(el('i', '', '粘贴 Cookie / Token，可先试连验证'));
      hand.appendChild(ht);
      var har = el('span', 'sheet-arw'); har.innerHTML = ICON('arrow');
      hand.appendChild(har);
      hand.addEventListener('click', function(){
        close();
        setTimeout(function(){ openManualSheet(p.id); }, 260);
      });
      body.appendChild(hand);
    });
  }

  /* ---------- 手动导入凭据 ---------- */
  function openManualSheet(providerId){
    openSheet('手动导入凭据', '从浏览器开发者工具复制 Cookie 或 Token 粘贴进来，不依赖网页登录。', function(body, close){
      var fProv = mkPlatformSelect(providerId);
      var fLabel = mkField('备注（选填）', 'input', { type: 'text', placeholder: '例如：主号' });
      var fCookie = mkField('Cookie', 'textarea', {
        placeholder: 'session=xxx; uid=xxx', rows: '3'
      }, '整段粘贴，多个用分号分隔。多数平台填这个就够。');
      var fToken = mkField('Access Token（选填）', 'input', {
        type: 'text', placeholder: 'token 型平台填这里'
      }, 'WorkBuddy 等平台用 token 而不是 Cookie。');
      var fKV = mkField('其他身份字段（选填）', 'textarea', {
        placeholder: 'uid=123\nenterprise_id=abc\ntenant_id=xx\ndomain=xxx', rows: '3'
      }, '每行一个 key=value；不确定就留空。');
      [fProv.wrap, fLabel.wrap, fCookie.wrap, fToken.wrap, fKV.wrap].forEach(function(w){ body.appendChild(w); });

      var result = el('div', 'sheet-sub', '');
      result.style.minHeight = '20px';
      body.appendChild(result);

      function payload(){
        var d = {
          provider: fProv.input.value,
          label: fLabel.input.value.trim(),
          cookie: fCookie.input.value.trim(),
          access_token: fToken.input.value.trim()
        };
        var kv = parseKV(fKV.input.value);
        for (var k in kv) d[k] = kv[k];
        return d;
      }

      var btns = el('div', 'row-btns');
      var btnProbe = el('button', 'btn ghost', '验证凭据');
      var btnSave = el('button', 'btn filled', '保存');
      btns.appendChild(btnProbe); btns.appendChild(btnSave);
      body.appendChild(btns);

      btnProbe.addEventListener('click', function(){
        var d = payload();
        if (!d.cookie && !d.access_token){ result.textContent = '先填 Cookie 或 Token'; return; }
        btnProbe.disabled = true; result.textContent = '正在试连…';
        post('/api/probe', d).then(function(snap){
          btnProbe.disabled = false;
          if (snap.error) result.textContent = '验证失败：' + snap.error;
          else result.textContent = '验证成功：余额 ' + fmt(snap.balance) + ' ' + (snap.unit || '') +
            (snap.checked_in ? '，今日已签' : '，今日未签');
        }).catch(function(e){
          btnProbe.disabled = false; result.textContent = '验证失败：' + e.message;
        });
      });

      btnSave.addEventListener('click', function(){
        var d = payload();
        if (!d.cookie && !d.access_token){ result.textContent = '先填 Cookie 或 Token'; return; }
        btnSave.disabled = true; result.textContent = '正在保存…';
        post('/api/account/add', d).then(function(r){
          btnSave.disabled = false;
          if (!r.ok){ result.textContent = '保存失败：' + (r.error || '未知错误'); return; }
          toast('已添加账号');
          close();
          load(true);
        }).catch(function(e){
          btnSave.disabled = false; result.textContent = '保存失败：' + e.message;
        });
      });
    });
  }

  /* ---------- 签到逻辑说明 ---------- */
  // 只说机制本身，不讲背景故事。用户原话：「只写你的签到逻辑就行了，
  // 没必要写那么一大堆」。所以压成三条，一屏能看完。
  function openHelp(){
    // 0.6.7：版本号从「设置」页迁到这里——它是本页唯一一处版本信息。
    // 顺带把 /api/meta 的结果存进 state.meta，供日志导出头复用（原来由设置页填充）。
    openSheet('签到逻辑', '三条，看完就清楚。', function(body, close){
      function para(title, text){
        body.appendChild(el('div', 'sect-h', title));
        var p = el('div', 'sheet-sub');
        p.style.margin = '0 0 14px';
        p.innerHTML = text;
        body.appendChild(p);
      }
      para('① 一天只自动签一次',
        '打开 App 先看今天有没有跑过：没跑过才做一次完整的' +
        '<b>签到 → 领每日任务奖励 → 抽奖</b>；跑过就只刷新数据，' +
        '并在顶部列出<b>哪几个没签到</b>，不会再发签到请求。');
      para('② 切后台回来什么都不会签',
        '回到前台只刷新列表。想补签或重试，点底部「一键签到」，' +
        '它每次都是真的跑一轮，不受「今天跑过」的限制。');
      para('③ 只处理「未签」的账号',
        '已签的账号不会再发请求。百度搭子的每日积分由平台在你当天第一次访问时发放，' +
        '所以我们看到它「已签」是真实状态。');

      var meta = el('div', 'meta', '版本 —');
      body.appendChild(meta);
      j('/api/meta').then(function(m){
        state.meta = m || null;
        meta.textContent = '版本 ' + ((m && m.version) || '—') +
          ' · 已支持 ' + ((m && m.providers) || 0) + ' 个平台';
      }).catch(function(){ /* 版本号取不到不影响使用，保持「版本 —」 */ });
    }, { hideCancel: true });
  }

  /* ---------- 设置（0.6.4：四个分页签） ----------
     用户要求：「设置里你的内容太复杂了，重新设计下 UI，要分页签」，
     并拍板分四个：通知 / 账号 / 数据 / 日志。

     「外观」不在这里——主题切换在主界面顶部，设置里不再重复一遍。
     每个 Tab 只放自己的东西，互不嵌套弹层，避免「点开设置又弹一层、
     还得回去再点」的来回折腾（这正是上一版被说「太复杂」的原因）。 */
  function openSettings(){
    var TABS = [
      { id: 'notify', name: '通知' },
      { id: 'account', name: '账号' },
      { id: 'data', name: '数据' },
      { id: 'log', name: '日志' }
    ];
    var cur = 'notify';

    // 0.6.7：「设置」标题、每页下方的版本号+平台数、底部关闭按钮全部去掉。
    // 版本号移到「签到逻辑」页。点遮罩仍可关闭本弹层。
    openSheet('', '', function(body, close){
      // 分页签：横向四等分，当前页签用实心胶囊，一眼知道在哪一页
      var bar = el('div', 'seg');
      var pane = el('div', 'tab-pane');
      var items = {};

      function pick(id){
        cur = id;
        for (var k in items) items[k].className = 'seg-item' + (k === id ? ' on' : '');
        pane.textContent = '';
        // 每次切页重画：保证进 Tab 拿的是最新数据（账号可能刚改过）
        (BUILDERS[id] || function(){}) (pane, close);
      }

      TABS.forEach(function(t){
        var b = el('button', 'seg-item' + (t.id === cur ? ' on' : ''), t.name);
        b.addEventListener('click', function(){ if (cur !== t.id) pick(t.id); });
        items[t.id] = b;
        bar.appendChild(b);
      });

      body.appendChild(bar);
      body.appendChild(pane);

      var BUILDERS = { notify: buildNotifyTab, account: buildAccountTab, data: buildDataTab, log: buildLogTab };
      pick(cur);
    }, { hideTitle: true, hideCancel: true });
  }

  // ---------- 通知 Tab ----------
  // 原来这一整块是「设置 → 签到通知 → 又弹一层」。现在直接摊在这一页里，
  // 进来就能看到「开没开、推到哪、地址对不对」。
  function buildNotifyTab(pane, close){
    var cfg = null;
    var loading = el('div', 'sheet-sub', '加载中…');
    pane.appendChild(loading);

    function loadCfg(){
      j('/api/notify/config').then(function(res){
        loading.remove();
        if (!res || res.ok === false){
          pane.appendChild(el('div', 'sheet-sub', (res && res.error) || '读取失败'));
          return;
        }
        cfg = res.config || {};
        render(res);
      }).catch(function(e){
        loading.textContent = '读取失败：' + e.message;
      });
    }

    function render(res){
      if (res.disabled){
        pane.appendChild(el('div', 'notice',
          '通知已被自动关闭：' + (res.last_error || 'Webhook 连续失败') +
          '。换个地址后点下面的「恢复通知」。'));
      } else if (res.fail_streak >= 3){
        pane.appendChild(el('div', 'notice',
          '最近连续 ' + res.fail_streak + ' 次推送失败：' + (res.last_error || '') +
          '。请检查地址是否还有效。'));
      }

      // 总开关
      var swRow = el('div', 'chk');
      var swT = el('div', 'chk-t');
      swT.appendChild(el('b', '', '启用通知'));
      swT.appendChild(el('i', '', '关闭后不发送任何推送，签到照常执行'));
      var sw = el('button', 'sw' + (cfg.enabled ? ' on' : ''));
      sw.addEventListener('click', function(){
        cfg.enabled = !cfg.enabled;
        sw.className = 'sw' + (cfg.enabled ? ' on' : '');
      });
      swRow.appendChild(swT); swRow.appendChild(sw);
      pane.appendChild(swRow);

      // 渠道：五个渠道挤在一行会换行错位，用 3 列网格
      pane.appendChild(el('div', 'sect-h', '渠道'));
      var seg = el('div', 'seg wrap');
      seg.style.flexWrap = 'wrap';
      NOTIFY_TYPES.forEach(function(t){
        var b = el('button', 'seg-item' + (cfg.type === t.id ? ' on' : ''), t.name.split('（')[0]);
        b.style.flex = '1 0 30%';
        b.addEventListener('click', function(){
          cfg.type = t.id;
          var all = seg.querySelectorAll('.seg-item');
          for (var i = 0; i < all.length; i++) all[i].classList.remove('on');
          b.classList.add('on');
          var picked = null;
          for (var k = 0; k < NOTIFY_TYPES.length; k++) if (NOTIFY_TYPES[k].id === t.id) picked = NOTIFY_TYPES[k];
          if (picked) fUrl.input.setAttribute('placeholder', picked.ph);
        });
        seg.appendChild(b);
      });
      pane.appendChild(seg);

      var curType = null;
      for (var i = 0; i < NOTIFY_TYPES.length; i++) if (NOTIFY_TYPES[i].id === cfg.type) curType = NOTIFY_TYPES[i];
      var fUrl = mkField('Webhook 地址', 'input', {
        type: 'text', placeholder: (curType && curType.ph) || '粘贴完整地址',
        value: cfg.url || ''
      }, '在群机器人设置里复制「Webhook 地址」整段粘贴进来。');
      fUrl.input.className = 'mono';
      pane.appendChild(fUrl.wrap);

      // 0.6.7：「什么时候推」四个开关全部删掉——用户拍板固定为
      // 「打开 App 自动签到 / 手动点一键签到 / always」三态全开，
      // 启用通知后每轮签到都推，不再有触发源/结果类型的分支。

      var btns = el('div', 'row-btns');
      var btnSave = el('button', 'btn filled', '保存');
      var btnTest = el('button', 'btn ghost', '发测试');
      btns.appendChild(btnSave); btns.appendChild(btnTest);
      pane.appendChild(btns);

      var out = el('div', 'sheet-sub', '');
      out.style.minHeight = '20px';
      pane.appendChild(out);

      if (res.disabled){
        var btnReset = el('button', 'btn ghost', '恢复通知');
        btnReset.style.width = '100%';
        btnReset.addEventListener('click', function(){
          post('/api/notify/reset', {}).then(function(r){
            if (!r || r.ok === false){ toast((r && r.error) || '恢复失败'); return; }
            toast('已恢复');
            pane.textContent = '';
            loadCfg();
          }).catch(function(e){ toast('恢复失败：' + e.message); });
        });
        pane.appendChild(btnReset);
      }

      function payload(){
        return {
          enabled: !!cfg.enabled, type: cfg.type || 'wecom',
          url: fUrl.input.value.trim(),
          // 触发开关已删，后端也固定恒推；这里写死 true 保持配置数据自洽
          on_success: true, on_failure: true, always: true,
          on_manual: true, on_start: true
        };
      }

      btnSave.addEventListener('click', function(){
        var p = payload();
        if (p.enabled && !p.url){ out.textContent = '启用通知需要先填 Webhook 地址'; return; }
        btnSave.disabled = true; out.textContent = '正在保存…';
        post('/api/notify/config', p).then(function(r){
          btnSave.disabled = false;
          if (!r || r.ok === false){ out.textContent = (r && r.error) || '保存失败'; return; }
          out.textContent = '';
          toast('已保存');
        }).catch(function(e){ btnSave.disabled = false; out.textContent = '失败：' + e.message; });
      });

      btnTest.addEventListener('click', function(){
        var p = payload();
        if (!p.url){ out.textContent = '先填 Webhook 地址'; return; }
        btnTest.disabled = true; out.textContent = '先保存再发送…';
        post('/api/notify/config', p).then(function(r){
          if (!r || r.ok === false){ btnTest.disabled = false; out.textContent = (r && r.error) || '保存失败'; return; }
          out.textContent = '正在发送测试消息…';
          return post('/api/notify/test', {});
        }).then(function(r){
          btnTest.disabled = false;
          if (!r) return;
          if (r.ok === false){ out.textContent = '发送失败：' + (r.error || '未知错误'); return; }
          out.textContent = '测试消息已发出，看看群里收到没有。';
        }).catch(function(e){ btnTest.disabled = false; out.textContent = '发送失败：' + e.message; });
      });

    }

    loadCfg();
  }

  // ---------- 账号 Tab（0.6.5 行内操作，0.6.6 加长按改备注） ----------
  // 以前点整行进详情页，退出来还得重新进设置 → 账号，用户明确抱怨过。
  // 现在整行不可点，三个操作摆在右边，一屏之内做完所有事：
  //   令牌（仅支持 TokenManager 的平台）/ 停用·启用 / 删除（连点两次确认）
  // 账号名长按可改备注（备注是用户自己起的识别名，不涉及凭据）。
  function bindLabelLongPress(node, a, refresh){
    var timer = null, fired = false, sx = 0, sy = 0;
    function start(x, y){
      fired = false; sx = x; sy = y;
      timer = setTimeout(function(){
        fired = true; timer = null;
        openLabelEdit(a, refresh);
      }, 500);
    }
    function cancel(){ if (timer){ clearTimeout(timer); timer = null; } }
    function moved(x, y){ return Math.abs(x - sx) > 10 || Math.abs(y - sy) > 10; }
    node.addEventListener('touchstart', function(e){
      var t = e.touches[0]; if (t) start(t.clientX, t.clientY);
    }, { passive: true });
    node.addEventListener('touchmove', function(e){
      var t = e.touches[0]; if (t && timer && moved(t.clientX, t.clientY)) cancel();
    }, { passive: true });
    node.addEventListener('touchend', cancel);
    node.addEventListener('touchcancel', cancel);
    node.addEventListener('mousedown', function(e){ start(e.clientX, e.clientY); });
    node.addEventListener('mousemove', function(e){ if (timer && moved(e.clientX, e.clientY)) cancel(); });
    node.addEventListener('mouseup', cancel);
    node.addEventListener('mouseleave', cancel);
    node.addEventListener('contextmenu', function(e){
      e.preventDefault(); cancel(); openLabelEdit(a, refresh);
    });
  }

  function openLabelEdit(a, refresh){
    openSheet('修改备注', platformName(a.provider) + ' · ' + (a.account_id || ''), function(body, close){
      var f = mkField('备注（留空则清除）', 'input', {
        type: 'text', maxlength: '40', placeholder: '例如：主号 / 小号',
        value: a.label || ''
      });
      f.input.value = a.label || '';
      body.appendChild(f.wrap);
      var btn = el('button', 'btn filled', '保存');
      btn.style.width = '100%';
      btn.style.marginTop = '12px';
      btn.addEventListener('click', function(){
        btn.disabled = true;
        post('/api/account/label', {
          provider: a.provider, account_id: a.account_id, label: f.input.value
        }).then(function(res){
          btn.disabled = false;
          if (!res || !res.ok){ toast((res && res.error) || '保存失败'); return; }
          toast('备注已更新'); close(); refresh(); load(true);
        }).catch(function(e){
          btn.disabled = false; toast('保存失败：' + e.message);
        });
      });
      body.appendChild(btn);
      setTimeout(function(){ try { f.input.focus(); } catch (e) {} }, 80);
    });
  }

  function buildAccountTab(pane){
    var box = el('div', '');
    pane.appendChild(box);

    var note = el('div', 'acc-note');
    function noteLine(k, txt){
      var d = el('div', '');
      d.appendChild(el('b', '', k));
      d.appendChild(document.createTextNode('　' + txt));
      note.appendChild(d);
    }
    noteLine('令牌', '平台发放的调用密钥（sk- 开头），用来代替登录凭据调用 API。点开可新建、复制、禁用。');
    noteLine('停用', '暂时不参与查询和签到，凭据原样保留。按钮会变成「启用」，随时点回来。');
    noteLine('删除', '把这个账号和它保存的凭据一起删掉，不可恢复。点一次变「确认删」，再点一次才真删，4 秒不点自动取消。');
    pane.appendChild(note);

    function refresh(){
      box.textContent = '';
      j('/api/accounts').then(function(list){
        box.textContent = '';
        if (!Array.isArray(list) || !list.length){
          box.appendChild(el('div', 'sheet-sub', '还没有账号，点主界面底部「添加账号」。'));
          return;
        }
        list.forEach(function(a){ box.appendChild(accountRow(a)); });
      }).catch(function(e){
        box.textContent = '';
        box.appendChild(el('div', 'sheet-sub', '读取账号失败：' + e.message));
      });
    }

    function accountRow(a){
      var off = a.enabled === false;
      var row = el('div', 'acc-row' + (off ? ' off' : ''));

      var dot = el('span', 'dot');
      var h = hueOf(a.provider);
      dot.style.background = 'linear-gradient(135deg,hsl(' + h + ',72%,58%),hsl(' + ((h + 34) % 360) + ',72%,50%))';
      dot.textContent = initials(platformName(a.provider));

      var info = el('div', 'acc-info');
      info.appendChild(el('b', '', platformName(a.provider) + (a.label ? ' · ' + a.label : '')));
      info.appendChild(el('i', '', (off ? '已停用 · ' : '') + a.account_id));
      info.title = '长按修改备注';
      bindLabelLongPress(info, a, refresh);

      var acts = el('div', 'acc-acts');

      // 令牌只给支持 TokenManager 的平台（现在只有 AiPy）
      if (hasTokens(a)){
        var bk = el('button', 'a-btn key', '令牌');
        bk.addEventListener('click', function(){ openTokens(a); });
        acts.appendChild(bk);
      }

      var bt = el('button', 'a-btn', off ? '启用' : '停用');
      bt.addEventListener('click', function(){
        bt.disabled = true;
        post('/api/account/toggle', {
          provider: a.provider, account_id: a.account_id, enabled: off
        }).then(function(res){
          bt.disabled = false;
          if (!res || !res.ok){ toast((res && res.error) || '操作失败'); return; }
          toast(off ? '已启用' : '已停用');
          refresh(); load(true);
        }).catch(function(e){
          bt.disabled = false; toast('操作失败：' + e.message);
        });
      });
      acts.appendChild(bt);

      // 删除：连点两次确认。不弹确认框，是因为弹层本身也是「点进去出不来」的一种
      var bd = el('button', 'a-btn warn', '删除');
      var armed = false, tmr = null;
      function disarm(){
        armed = false;
        if (tmr){ clearTimeout(tmr); tmr = null; }
        bd.textContent = '删除'; bd.className = 'a-btn warn'; bd.disabled = false;
      }
      bd.addEventListener('click', function(){
        if (!armed){
          armed = true;
          bd.textContent = '确认删'; bd.className = 'a-btn arm';
          tmr = setTimeout(disarm, 4000);
          return;
        }
        bd.disabled = true; bd.textContent = '删除中';
        post('/api/account/delete', { provider: a.provider, account_id: a.account_id })
          .then(function(res){
            if (!res || !res.ok){
              toast((res && res.error) || '删除失败');
              disarm();
              return;
            }
            toast('已删除账号');
            refresh(); load(true);
          }).catch(function(e){
            toast('删除失败：' + e.message);
            disarm();
          });
      });
      acts.appendChild(bd);

      row.appendChild(dot); row.appendChild(info); row.appendChild(acts);
      return row;
    }

    refresh();
  }

  // ---------- 数据 Tab ----------
  // 备份导出 / 导入：整块搬过来，只是从「设置页往下滚很久」变成「点一下就到」。
  function buildDataTab(pane){
    pane.appendChild(el('div', 'sect-h', '导出'));
    var fExp = mkField('备份内容（可长按全选复制）', 'textarea', {
      readonly: 'readonly', rows: '5', placeholder: '点下面的「生成」'
    });
    fExp.input.className = 'mono';
    pane.appendChild(fExp.wrap);

    // 0.6.8：三个按钮合成一行三等分，顺序固定为 生成 → 复制 → 导出。
    // 以前「复制到剪贴板」独占一整行，占地方还得往下滚，用户要求收紧成一行。
    var expBtns = el('div', 'row-btns');
    var btnExp = el('button', 'btn ghost', '生成');
    var btnCopy = el('button', 'btn ghost', '复制');
    var btnFile = el('button', 'btn ghost', '导出');
    expBtns.appendChild(btnExp); expBtns.appendChild(btnCopy); expBtns.appendChild(btnFile);
    pane.appendChild(expBtns);

    btnExp.addEventListener('click', function(){
      j('/api/export').then(function(d){
        fExp.input.value = JSON.stringify(d, null, 2);
        toast('备份已生成（' + (d.accounts || []).length + ' 个账号）');
      }).catch(function(e){ toast('导出失败：' + e.message); });
    });
    btnFile.addEventListener('click', function(){ saveBackupFile(); });
    btnCopy.addEventListener('click', function(){ copyText(fExp.input); });

    pane.appendChild(el('div', 'sect-h', '导入'));
    var fImp = mkField('把备份 JSON 粘贴到这里', 'textarea', {
      rows: '5', placeholder: '{"version":1,"accounts":[...]}'
    });
    fImp.input.className = 'mono';
    pane.appendChild(fImp.wrap);

    var btnPick = el('button', 'btn ghost', '选择备份文件');
    btnPick.style.width = '100%';
    btnPick.addEventListener('click', function(){
      pickBackupFile(function(text){
        fImp.input.value = text;
        toast('已读取文件，点「导入备份」应用');
      });
    });
    pane.appendChild(btnPick);

    var btnImp = el('button', 'btn filled', '导入备份');
    btnImp.style.width = '100%';
    btnImp.style.marginTop = '10px';
    btnImp.addEventListener('click', function(){
      var txt = fImp.input.value.trim();
      if (!txt){ toast('先粘贴备份内容或选择备份文件'); return; }
      btnImp.disabled = true;
      post('/api/import', { text: txt }).then(function(r){
        btnImp.disabled = false;
        if (!r.ok){ toast('导入失败：' + (r.error || '未知错误')); return; }
        toast('导入成功：新增 ' + r.added + (r.skipped ? '，跳过 ' + r.skipped : ''));
        load(true);
      }).catch(function(e){
        btnImp.disabled = false; toast('导入失败：' + e.message);
      });
    });
    pane.appendChild(btnImp);
  }

  // ---------- 日志 Tab ----------
  // 0.6.4：按执行步骤逐条记录（含成功请求），每轮共用 #轮次号。
  // 默认「全部」，因为用户要看的正是「它到底跑了什么」。
  function buildLogTab(pane){
    var logBar = el('div', 'row-btns');
    var btnLogAll = el('button', 'btn filled', '全部');
    var btnLogErr = el('button', 'btn ghost', '只看异常');
    logBar.appendChild(btnLogAll); logBar.appendChild(btnLogErr);
    pane.appendChild(logBar);

    var logBox = el('div', 'logbox');
    pane.appendChild(logBox);

    var logBtns = el('div', 'row-btns');
    var btnLogRefresh = el('button', 'btn ghost', '刷新');
    var btnLogCopy = el('button', 'btn ghost', '复制');
    var btnLogClear = el('button', 'btn ghost', '清空');
    logBtns.appendChild(btnLogRefresh);
    logBtns.appendChild(btnLogCopy);
    logBtns.appendChild(btnLogClear);
    pane.appendChild(logBtns);

    var lastLogs = [];
    var onlyErr = false;

    function applyFilter(){
      btnLogAll.className = 'btn ' + (onlyErr ? 'ghost' : 'filled');
      btnLogErr.className = 'btn ' + (onlyErr ? 'filled' : 'ghost');
    }

    // 网络错误的消息里常带完整 URL（Go 的 *url.Error 就这么拼），
    // 而路径上一行已经显示了，重复一长串只会把真正的原因挤出屏幕。
    function trimURL(text){
      if (!text) return '';
      return text.replace(/https?:\/\/[^\s"']+/g, function(u){
        var i = u.indexOf('/', u.indexOf('//') + 2);
        return i > 0 ? u.slice(i) : u;
      });
    }

    function logLine(e){
      var row = el('div', 'logrow' + (e.level && e.level !== 'info' ? ' ' + e.level : ''));
      row.appendChild(el('span', 'lt', e.time || ''));
      var msg = el('span', 'lm');
      // 第一行：#轮次 + 步骤。step 是后端给的人话步骤名（「签到 百度搭子」
      // 「领任务奖励」），比只显示 HTTP 方法+路径更接近用户看到的界面动作。
      var head = [];
      if (e.round) head.push('#' + e.round);
      if (e.step) head.push(e.step);
      else {
        if (e.scope && e.scope !== 'http') head.push(e.scope);
        if (e.method) head.push(e.method);
        if (e.path) head.push(e.path);
      }
      msg.appendChild(el('b', '', head.join(' ')));
      var tail = [];
      if (e.status) tail.push('HTTP ' + e.status);
      if (e.code) tail.push('code ' + e.code);
      // 耗时一律显示：用户要看的就是「每一步花了多久」
      if (e.cost_ms) tail.push(e.cost_ms + 'ms');
      if (e.msg) tail.push(trimURL(e.msg));
      else if (e.resp) tail.push(trimURL(e.resp));
      if (tail.length) msg.appendChild(el('div', 'sub', tail.join(' · ')));
      row.appendChild(msg);
      return row;
    }

    function renderLogs(){
      j('/api/logs?limit=600').then(function(res){
        var all = (res && res.logs) || [];
        var logs = onlyErr ? all.filter(function(e){
          return e.level && e.level !== 'info';
        }) : all;
        lastLogs = logs;
        logBox.textContent = '';
        if (!logs.length){
          logBox.appendChild(el('div', 'logempty',
            onlyErr ? '没有异常的请求记录。' : '暂时没有日志。'));
          return;
        }
        // 接口返回的是倒序（最新在前），直接顺序渲染即可，
        // 不需要滚动到底——用户第一眼就该看到刚发生的事。
        for (var i = 0; i < logs.length; i++){ logBox.appendChild(logLine(logs[i])); }
      }).catch(function(e){
        logBox.textContent = '';
        logBox.appendChild(el('div', 'logempty', '读取日志失败：' + e.message));
      });
    }

    applyFilter();
    btnLogAll.addEventListener('click', function(){
      if (!onlyErr) return;
      onlyErr = false; applyFilter(); renderLogs();
    });
    btnLogErr.addEventListener('click', function(){
      if (onlyErr) return;
      onlyErr = true; applyFilter(); renderLogs();
    });
    btnLogRefresh.addEventListener('click', function(){ renderLogs(); });
    btnLogClear.addEventListener('click', function(){
      post('/api/logs/clear', {}).then(function(){
        toast('日志已清空'); renderLogs();
      }).catch(function(e){ toast('清空失败：' + e.message); });
    });
    btnLogCopy.addEventListener('click', function(){
      if (!lastLogs.length){ toast('暂时没有日志可复制'); return; }
      var lines = [];
      for (var i = 0; i < lastLogs.length; i++){
        var e = lastLogs[i];
        var seg = [e.date || '', e.time || '', (e.level || 'info').toUpperCase()];
        if (e.round) seg.push('[' + e.round + ']');
        if (e.step) seg.push(e.step);
        if (e.scope) seg.push(e.scope);
        if (e.method) seg.push(e.method);
        if (e.path) seg.push(e.path);
        if (e.status) seg.push('HTTP ' + e.status);
        if (e.code) seg.push('code ' + e.code);
        if (e.cost_ms) seg.push(e.cost_ms + 'ms');
        var line = seg.join(' ');
        var detail = e.msg || e.resp || '';
        if (detail) line += ' | ' + detail;
        lines.push(line);
      }
      // 导出头带上版本与设备信息：贴出去求助时对方一眼知道是哪个环境
      var head = '积分矩阵 运行日志（' + lastLogs.length + ' 条）\n';
      head += '版本 ' + ((state.meta && state.meta.version) || '—') +
              ' · 设备 ' + (navigator.userAgent || '未知') + '\n';
      var off = new Date().getTimezoneOffset();
      head += '时间 ' + new Date().toLocaleString() + '（UTC' +
              (off > 0 ? '-' : '+') + Math.abs(off / 60) + '）\n';
      var ta = document.createElement('textarea');
      ta.value = head + lines.join('\n');
      document.body.appendChild(ta);
      copyText(ta);
      document.body.removeChild(ta);
    });

    renderLogs();
  }

  /* ---------- 每日任务（AiPy 等支持任务的平台） ---------- */
  function openTasks(r){
    var name = platformName(r.provider);
    var box = null;
    var s = openSheet(name + ' · 每日任务', r.label || r.account_id || '', function(body, close){
      box = el('div', '');
      body.appendChild(box);
      box.appendChild(el('div', 'sheet-sub', '加载中…'));
      loadTasks();
    });

    function loadTasks(){
      j('/api/tasks?account=' + encodeURIComponent(r.account_id)).then(function(res){
        if (!box) return;
        box.textContent = '';
        var tasks = (res && res.tasks) || [];
        if (!tasks.length){
          box.appendChild(el('div', 'sheet-sub', res && res.error ? res.error : '当前没有可展示的任务'));
          return;
        }
        tasks.forEach(function(t){
          var row = el('div', 'task-row');
          var info = el('div', 'task-info');
          var t1 = el('div', 'task-title');
          t1.textContent = t.title || t.task_type || '任务';
          info.appendChild(t1);
          var t2 = el('div', 'task-sub');
          var rewardTxt = (t.reward > 0 ? '+' + fmt(t.reward) + ' ' + (t.unit || '') : '');
          var progressTxt = (t.target > 0 ? t.progress + '/' + t.target : '');
          t2.textContent = [rewardTxt, progressTxt, t.status === 'claimed' ? '已领取' : ''].filter(Boolean).join(' · ');
          info.appendChild(t2);
          row.appendChild(info);

          var act = el('button', 'mini' + (t.claimable ? ' go' : ''));
          if (t.status === 'claimed'){
            act.textContent = '已领'; act.disabled = true;
          } else if (t.claimable){
            act.textContent = '领取';
          } else {
            act.textContent = '未达标'; act.disabled = true;
          }
          if (t.claimable){
            act.addEventListener('click', function(){
              act.disabled = true; act.textContent = '领取中…';
              post('/api/tasks/claim', { account: r.account_id, task_type: t.task_type })
                .then(function(res){
                  if (res && res.error){ toast(res.error); act.disabled = false; act.textContent = '领取'; }
                  else {
                    toast('领取成功' + gainTxt(res));
                    loadTasks();
                    load(true);
                  }
                })
                .catch(function(e){ toast('领取失败：' + e.message); act.disabled = false; act.textContent = '领取'; });
            });
          }
          row.appendChild(act);
          box.appendChild(row);
        });
      }).catch(function(e){
        if (!box) return;
        box.textContent = '';
        box.appendChild(el('div', 'sheet-sub', '读取任务失败：' + e.message));
      });
    }
  }

  /* ---------- 抽奖（百度搭子等） ----------
     用户原话：「做了抽奖但是没做按钮啊，我不能点按钮进去看抽奖状态和抽到什么奖，
     没有手动领取入口」。所以这个弹层要给出三件事：
       1) 还剩几次机会（大字，一眼看到）
       2) 抽到了什么（本次会话的奖品列表）
       3) 手动抽一次的按钮（次数为 0 时禁用并说明原因） */
  function openLottery(r){
    var name = platformName(r.provider);
    var box = null, remainEl = null, prizeBox = null, drawBtn = null;
    var history = [];

    openSheet(name + ' · 抽奖', r.label || r.account_id || '', function(body, close){
      // 顶部状态卡
      var card = el('div', 'lot-card');
      remainEl = el('div', 'lot-remain', '…');
      var cap = el('div', 'lot-cap', '剩余抽奖机会');
      card.appendChild(cap);
      card.appendChild(remainEl);
      body.appendChild(card);

      prizeBox = el('div', 'lot-prizes');
      body.appendChild(prizeBox);

      drawBtn = el('button', 'btn filled sm', '立即抽奖');
      drawBtn.style.width = '100%';
      drawBtn.disabled = true;

      // setRemain 必须在按钮的 click 闭包里可用，所以定义在监听器之前
      function setRemain(n){
        remainEl.textContent = String(n == null ? '—' : n);
        remainEl.className = 'lot-remain' + (n > 0 ? ' on' : '');
        drawBtn.disabled = !(n > 0);
        drawBtn.textContent = n > 0 ? '立即抽奖' : '暂无抽奖机会';
      }
      function renderPrizes(){
        prizeBox.textContent = '';
        if (!history.length){
          prizeBox.appendChild(el('div', 'lot-empty', '还没抽过。抽中的奖品会显示在这里。'));
          return;
        }
        history.forEach(function(it){
          var row = el('div', 'lot-prize');
          var ic = el('div', 'lot-ic'); ic.innerHTML = ICON('trophy');
          row.appendChild(ic);
          var tx = el('div', 'lot-tx');
          tx.appendChild(el('b', '', it.prize));
          if (it.gain) tx.appendChild(el('i', '', it.gain));
          row.appendChild(tx);
          prizeBox.appendChild(row);
        });
      }

      drawBtn.addEventListener('click', function(){
        drawBtn.disabled = true;
        drawBtn.textContent = '抽奖中…';
        post('/api/lottery/draw', { account: r.account_id }).then(function(res){
          if (res && res.error && !res.ok){
            toast(res.error);
          } else if (res && res.ok){
            // 局部变量改名，避免遮蔽同名工具函数 gainTxt()（var 会提升到函数作用域，
            // 遮蔽后同函数里再调 gainTxt() 就会 TypeError）
            var gText = res.gain ? '+' + fmt(res.gain) + ' ' + (res.unit || '积分') : '';
            history.unshift({ prize: res.prize || '抽中一次', gain: gText });
            toast('抽中：' + (res.prize || '奖励') + (gText ? '，' + gText : ''));
          } else {
            toast('没有抽到奖励');
          }
          if (res && res.remaining != null) setRemain(res.remaining);
          renderPrizes();
          drawBtn.textContent = '立即抽奖';
          drawBtn.disabled = !(res && res.remaining > 0);
          load(true);
        }).catch(function(e){
          toast('抽奖失败：' + e.message);
          drawBtn.textContent = '立即抽奖';
          drawBtn.disabled = false;
        });
      });
      body.appendChild(drawBtn);

      var tip = el('div', 'sheet-sub',
        '签到会送抽奖机会；「一键签到」会自动把这些机会抽掉，这里可以看结果或补抽。');
      tip.style.paddingTop = '10px';
      body.appendChild(tip);

      // refresh 定义在回调内部：它要用 setRemain，而 setRemain 依赖这里的 DOM 引用。
      // 放到回调外面会变成 ReferenceError（实测踩过：面板一直显示 —）。
      function refresh(){
        j('/api/lottery/status?account=' + encodeURIComponent(r.account_id)).then(function(res){
          if (!remainEl) return;
          if (res && res.ok){ setRemain(res.remaining); }
          else {
            remainEl.textContent = '—';
            remainEl.className = 'lot-remain';
            drawBtn.disabled = true;
            drawBtn.textContent = '无法查询';
            toast((res && res.error) || '查询失败');
          }
        }).catch(function(e){
          if (!remainEl) return;
          remainEl.textContent = '—';
          drawBtn.disabled = true;
          toast('查询失败：' + e.message);
        });
      }

      renderPrizes();
      refresh();
    });

  }

  /* ---------- 通知设置（0.6.0） ----------
     粒度按用户要求：一次一键签到只推一条汇总。
     渠道五个全给：企业微信 / 钉钉 / 飞书 / Bark / 自定义。
     为什么必须在 Go 侧发：JS fetch 会带本机回环 Origin 头，
     企业微信和钉钉的网关会直接拒收；而且 App 切后台时 JS 请求会被掐断。
     所以前端只负责把「这一轮的结果」整理好交给 /api/notify。 */
  var NOTIFY_TYPES = [
    { id: 'wecom', name: '企业微信机器人', ph: 'qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...' },
    { id: 'dingtalk', name: '钉钉机器人', ph: 'oapi.dingtalk.com/robot/send?access_token=...' },
    { id: 'feishu', name: '飞书机器人', ph: 'open.feishu.cn/open-apis/bot/v2/hook/...' },
    { id: 'bark', name: 'Bark（iOS 推送）', ph: 'api.day.app/你的Key' },
    { id: 'custom', name: '自定义 Webhook', ph: 'example.com/hook' }
  ];


  /* ---------- 卡片右上角 ···：已删除（0.6.4） ----------
     0.6.5：连账号详情也删了。用户的原话是「点进详情之后我就返不回去了，
     还得再从设置里面进去一遍，这个设计得很让人蛋疼」。
     现在令牌 / 停用 / 删除全部摆在设置 → 账号的行内，不再有二级页。 */

  /* ---------- API 令牌管理（0.6.0，仅 AiPy 等支持平台） ----------
     用户要求：列表要显示 model_limits / allow_ips，且可编辑。
     删除保护由后端把关（key 正好是当前登录凭证时拒绝），前端在这里
     把「正在使用」标出来，并把它做成一次明确的二次确认。 */
  function openTokens(r){
    var name = platformName(r.provider);
    var box = null, inUse = '', usable = false;

    openSheet(name + ' · API 令牌', r.label || r.account_id || '', function(body, close){
      // 说明条用中性色：这里没有风险，用警示黄会把整页的注意力都吸走
      var tip = el('div', 'notice soft',
        '令牌是平台发放的调用密钥（sk- 开头），和登录凭据是两回事。点「复制」拷走整条 key。');
      body.appendChild(tip);

      var bar = el('div', 'row-btns');
      var btnNew = el('button', 'btn filled', '新建令牌');
      var btnRe = el('button', 'btn ghost', '刷新');
      bar.appendChild(btnNew); bar.appendChild(btnRe);
      body.appendChild(bar);

      box = el('div', '');
      box.style.marginTop = '12px';
      body.appendChild(box);

      btnRe.addEventListener('click', function(){ href(); });
      btnNew.addEventListener('click', function(){
        // 不再用 usable 提前拦截。
        //
        // 0.6.0 在这里 if (!usable) 直接 toast 一句就 return —— 而 usable 当时
        // 被后端错算成「有没有令牌在用」，Cookie 登录的账号恒为 false，
        // 于是「点新建没有任何反应」（只闪一下 toast，看着像没点动）。
        // 后端判定已修，这里也改成：拦不住就让用户真的试一次，
        // 失败时把上游返回的真实原因显示出来，比一句笼统的提示有用得多。
        openTokenCreate(r, function(){ href(); });
      });

      function href(){ reload(); }

      function reload(){
        box.textContent = '';
        box.appendChild(el('div', 'sheet-sub', '加载中…'));
        j('/api/tokens?provider=' + encodeURIComponent(r.provider) +
          '&account=' + encodeURIComponent(r.account_id)).then(function(res){
          box.textContent = '';
          if (!res || res.ok === false){
            box.appendChild(el('div', 'sheet-sub', (res && res.error) || '读取失败'));
            return;
          }
          inUse = res.in_use_key || '';
          usable = !!res.usable;
          renderList(res.tokens || []);
        }).catch(function(e){
          box.textContent = '';
          box.appendChild(el('div', 'sheet-sub', '读取失败：' + e.message));
        });
      }

      // 原来的 maskKey() 已删除：它对 key 做首尾截断显示，正是
      // 「看不到 apikey」的直接原因。现在列表原样透出上游给的 key。

      function renderList(list){
        if (!list.length){
          box.appendChild(el('div', 'sheet-sub', '还没有令牌。点「新建令牌」创建一条。'));
          return;
        }
        // 在用且在启用的排前面：用户最关心「我这条还能用吗」
        list.sort(function(a, b){
          var ai = inUse && a.key === inUse ? 1 : 0;
          var bi = inUse && b.key === inUse ? 1 : 0;
          if (ai !== bi) return bi - ai;
          return (b.created_time || 0) - (a.created_time || 0);
        });
        list.forEach(function(t){
          var card = el('div', 'tok' + (t.status === 1 ? '' : ' off'));

          var h = el('div', 'tok-h');
          var nm = el('b', '', t.name || '未命名');
          var st = el('span', 'st', t.status === 1 ? '启用' : '禁用');
          h.appendChild(nm); h.appendChild(st);
          if (inUse && t.key === inUse){
            var tag = el('span', 'st', '在用');
            tag.style.background = 'var(--primary-c)';
            tag.style.color = 'var(--on-primary-c)';
            h.appendChild(tag);
          }
          card.appendChild(h);

          // key 行：完整显示。
          //
          // 0.6.0 / 0.6.1 这里一直套着 maskKey()，显示成「sk-Zx9Q····7890」，
          // 用户看到的就是「没有显示 apikey」——脱敏之后这条信息完全没法用。
          // 令牌列表是本人的私有页面，key 本来就要拿去填到别的客户端里，
          // 完整透出才是它唯一的用处。复制按钮在下面的操作行里。
          var keyTxt = el('div', 'tok-key', t.key || '（创建时未返 key，可能已失效）');
          keyTxt.title = t.key || '';
          card.appendChild(keyTxt);

          var meta = el('div', 'tok-meta');
          var quota = t.unlimited_quota ? '不限额度' :
            ('剩余 ' + fmt(t.remain_quota) + ' / 已用 ' + fmt(t.used_quota));
          var exp = t.expired_time < 0 ? '永不过期' : ('到期 ' + fmtDay(t.expired_time));
          meta.appendChild(el('b', '', quota));
          meta.appendChild(document.createTextNode(' · ' + exp));
          card.appendChild(meta);

          if (t.model_limits) card.appendChild(el('div', 'tok-lim', '模型限制：' + t.model_limits));
          if (t.allow_ips) card.appendChild(el('div', 'tok-lim', 'IP 白名单：' + t.allow_ips));

          var act = el('div', 'tok-act');

          // 复制放在操作行的第一位，和「禁用 / 删除」排在一起。
          // 之前它挂在 key 行右端，同一组按钮被拆成上下两处，看着很散。
          if (t.key){
            var bCopy = el('button', 'tok-btn', '复制');
            bCopy.setAttribute('aria-label', '复制令牌');
            bCopy.addEventListener('click', function(){
              copyToken(t.key, bCopy);
            });
            act.appendChild(bCopy);
          }

          // 编辑已移除：令牌只有「名称 / 额度 / 限制」几个字段，
          // 实际改动需求几乎为零，而编辑面板占满一屏反而把列表挤没了
          // （用户原话：「编辑功能删掉吧，在这里确实没什么用」）。
          // 真要改额度，删了重建更直接。
          var bSw = el('button', 'tok-btn', t.status === 1 ? '禁用' : '启用');
          bSw.addEventListener('click', function(){
            bSw.disabled = true;
            post('/api/tokens/status', {
              provider: r.provider, account: r.account_id, id: t.id,
              status: t.status === 1 ? 2 : 1
            }).then(function(res){
              bSw.disabled = false;
              if (!res || res.ok === false){ toast((res && res.error) || '操作失败'); return; }
              toast(t.status === 1 ? '已禁用' : '已启用');
              reload();
            }).catch(function(e){ bSw.disabled = false; toast('操作失败：' + e.message); });
          });
          act.appendChild(bSw);

          // 删除：两段式。第一下 armed，第二下才真删；若后端回 in_use
          // 再弹一次「确实要删」的最后机会（force=true）。
          var armed = false, timer = null;
          var bDel = el('button', 'tok-btn danger', '删除');
          bDel.addEventListener('click', function(){
            if (!armed){
              armed = true; bDel.textContent = '确认删除';
              timer = setTimeout(function(){ armed = false; bDel.textContent = '删除'; }, 4000);
              return;
            }
            clearTimeout(timer);
            doDelete(t, false);
          });
          act.appendChild(bDel);
          card.appendChild(act);
          box.appendChild(card);
        });
      }

      function doDelete(t, force){
        post('/api/tokens/delete', {
          provider: r.provider, account: r.account_id, id: t.id, key: t.key, force: !!force
        }).then(function(res){
          if (res && res.in_use){
            // 后端判定「这条正是当前登录凭证」。再问一次，说明后果。
            var ok = window.confirm(
              '这条令牌正是该账号当前登录使用的凭证。\n删除后账号会立刻失效，需要重新登录。\n\n确实要删除吗？');
            if (ok) doDelete(t, true);
            else toast('已取消');
            return;
          }
          if (!res || res.ok === false){ toast((res && res.error) || '删除失败'); return; }
          toast('已删除');
          reload();
        }).catch(function(e){ toast('删除失败：' + e.message); });
      }

      reload();
    });
  }

  function fmtDay(sec){
    if (!sec) return '—';
    // 上游有的平台返秒、有的返毫秒，13 位按毫秒处理，否则会算出公元 59009 年
    if (sec > 1e12) sec = Math.floor(sec / 1000);
    var d = new Date(sec * 1000);
    function p(n){ return (n < 10 ? '0' : '') + n; }
    return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate());
  }

  /* 新建 / 编辑令牌。t 为 null 表示新建。 */
  // 新建令牌。编辑功能已按用户要求移除——令牌的字段就那么几个，
  // 真要改额度不如删了重建，而编辑面板占满一屏反而把列表挤没了。
  function openTokenCreate(r, done){
    openSheet('新建令牌', '名称必填；其余留空表示沿用平台默认。',
      function(body, close){
        var fName = mkField('名称', 'input', {
          type: 'text', placeholder: '例如：手机端'
        });
        var fQuota = mkField('额度（选填）', 'input', {
          type: 'text', placeholder: '留空 = 不限'
        }, '填数字表示剩余可用额度；留空则不限额度。');
        var fModel = mkField('模型限制（选填）', 'input', {
          type: 'text', placeholder: '例如：gpt-4o,claude-3-5'
        }, '逗号分隔。留空表示不限制可用模型。');
        var fIPs = mkField('IP 白名单（选填）', 'input', {
          type: 'text', placeholder: '例如：1.2.3.4,5.6.7.*'
        }, '逗号分隔，支持前缀匹配。留空表示不限制来源 IP。');

        [fName.wrap, fQuota.wrap, fModel.wrap, fIPs.wrap].forEach(function(w){ body.appendChild(w); });

        var expHint = el('div', 'sheet-sub', '有效期：默认永不过期');
        expHint.style.paddingTop = '0';
        body.appendChild(expHint);

        var out = el('div', 'sheet-sub', '');
        out.style.minHeight = '20px';
        body.appendChild(out);

        var btns = el('div', 'row-btns');
        var btnSave = el('button', 'btn filled', '创建');
        btns.appendChild(btnSave);
        body.appendChild(btns);

        btnSave.addEventListener('click', function(){
          var nm = fName.input.value.trim();
          if (!nm){ out.textContent = '名称不能为空'; return; }
          var q = fQuota.input.value.trim();
          var quota = q === '' ? 0 : Number(q);
          if (q !== '' && (isNaN(quota) || quota < 0)){ out.textContent = '额度要填非负数字，或留空'; return; }

          btnSave.disabled = true;
          out.textContent = '正在创建…';
          post('/api/tokens/save', {
            provider: r.provider, account: r.account_id,
            token: {
              name: nm,
              remain_quota: quota,
              unlimited_quota: q === '',
              model_limits: fModel.input.value.trim(),
              allow_ips: fIPs.input.value.trim()
            }
          }).then(function(res){
            btnSave.disabled = false;
            if (!res || res.ok === false){
              // 把上游的真实原因摊开：网络错误 / 权限不足 / 上游拒绝各不相同，
              // 笼统一句「操作失败」等于让用户去猜。
              out.textContent = (res && res.error) || '创建失败';
              return;
            }
            if (res.created && res.token && res.token.key){
              showNewKey(close, res.token);
              done();
              return;
            }
            toast('已创建');
            close();
            done();
          }).catch(function(e){
            btnSave.disabled = false;
            out.textContent = '失败：' + (e && e.message ? e.message : '网络错误');
          });
        });
      });
  }

  /* 新建成功后单独弹一次，把完整 key 摆出来并可一键复制。 */
  function showNewKey(prevClose, tok){
    try { prevClose(); } catch (e) {}
    setTimeout(function(){
      openSheet('令牌已创建',
        '这是唯一一次显示完整 key 的机会，请现在复制保存。',
        function(body, close){
          var f = mkField('完整 key', 'textarea', { readonly: 'readonly', rows: '3' });
          f.input.className = 'mono';
          f.input.value = tok.key || '';
          body.appendChild(f.wrap);

          var btns = el('div', 'row-btns');
          var btnCopy = el('button', 'btn filled', '复制 key');
          var btnOk = el('button', 'btn ghost', '我已保存');
          btns.appendChild(btnCopy); btns.appendChild(btnOk);
          body.appendChild(btns);

          btnCopy.addEventListener('click', function(){
            copyText(f.input);
          });
          btnOk.addEventListener('click', close);
        });
    }, 260);
  }

  // 让壳层打开官方登录页。
  // Android: WebView.addJavascriptInterface 注入 window.PointsBridge.openLogin(id)
  // iOS:     WKScriptMessageHandler 同名；都不可用时退回自定义 scheme
  function startLogin(providerId){
    // 记账必须放在任何 return 之前：原先写在函数末尾，iOS / Android 走桥时
    // 会提前 return，时间戳根本没写上，resumeAfterLogin 于是什么都不做。
    loginPendingAt = Date.now();
    // WorkBuddy 要 OAuth：标记持久化到 localStorage，壳层 reload（页面重载）
    // 之后内存变量会丢，只有存下来才能续上换取 token 的流程。
    try {
      if (providerId === 'workbuddy') localStorage.setItem('oauth_pending', 'workbuddy');
      else localStorage.removeItem('oauth_pending');
    } catch (e) {}
    // 三条通路，按可用性依次尝试：
    // 1) iOS：WKScriptMessageHandler，需要 postMessage 形式
    try {
      if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.PointsBridge){
        window.webkit.messageHandlers.PointsBridge.postMessage({ action: 'openLogin', provider: providerId });
        return;
      }
    } catch (e) {}
    // 2) Android：addJavascriptInterface 注入的同步方法
    try {
      if (window.PointsBridge && typeof window.PointsBridge.openLogin === 'function'){
        window.PointsBridge.openLogin(providerId);
        return;
      }
    } catch (e) {}
    // 3) 兜底：自定义 scheme，两端 WebViewClient / navigationDelegate 都会拦
    location.href = 'points://login?provider=' + encodeURIComponent(providerId);
  }

  /* ---------- 对外暴露给壳层：打开即签到 ---------- */
  // 两个入口分工明确（0.5.5 起）：
  //   __pointsAutoCheckin —— 冷启动专用：等同点一次「一键签到」（签到+领奖+抽奖）
  //   __pointsRefresh     —— 回前台专用：只刷新列表，绝不签到
  // 壳层负责区分「冷启动」和「回到前台」，页面侧再按调用名执行对应动作，
  // 避免一次下午切十次后台就签十轮，把账号送进风控名单。
  var loginPendingAt = 0;

  // finishOAuth：WorkBuddy 这类 token 型平台，登录完成只是第一步，
  // 还要让引擎拿一次性 state 去上游换 access token。
  // 后端每次只查一次（避免长连接被 25s 超时打断），等待时前端隔 2.5s 再来。
  function finishOAuth(provider){
    var tries = 0;
    toast('正在换取登录凭证…');
    function tick(){
      tries++;
      post('/api/oauth/finish', { provider: provider }).then(function(r){
        if (r && r.ok){
          toast('账号已添加' + (r.nickname ? '：' + r.nickname : ''));
          load(true);
          return;
        }
        if (r && r.waiting && tries < 24){ setTimeout(tick, 2500); return; }
        toast((r && r.error) || '登录未完成，请重试');
      }).catch(function(e){ toast('登录失败：' + e.message); });
    }
    tick();
  }

  // resumeAfterLogin：登录容器关闭后回到前台时调用。
  // 只要距上次发起登录不到 10 分钟，就重新拉一次数据（新增账号会立刻出现）。
  function resumeAfterLogin(){
    // OAuth 型平台优先：登录完成 ≠ 拿到 token，必须再走一次换取流程
    var pend = '';
    try { pend = localStorage.getItem('oauth_pending') || ''; } catch (e) {}
    if (pend === 'workbuddy'){
      try { localStorage.removeItem('oauth_pending'); } catch (e) {}
      loginPendingAt = 0;
      finishOAuth('workbuddy');
      return;
    }
    if (!loginPendingAt) return;
    if (Date.now() - loginPendingAt > 10 * 60 * 1000) { loginPendingAt = 0; return; }
    loginPendingAt = 0;
    load(true).then(function(){ toast('已同步账号'); });
  }
  // 启动逻辑（0.6.4 重做）
  //
  // 用户拍板的规则只有一条：
  //   「探测今天是否有签到日志，有就只刷新数据并提示哪几个没签、哪些奖励没领，
  //     没有签到日志就直接走一遍一键签到。」
  //
  // 为什么这么改：自动签到是风控的高危动作，一天跑一次就够；
  // 一天开几次 App 就签几次，等于自己往黑号名单里走。
  //
  // 「今天跑过没有」由引擎侧落盘记录（/api/daily），不是前端记内存——
  // 进程会被系统随时杀掉，内存留不住，而那正是「一天重复签好几轮」的来源。
  //
  // 0.6.4 同时删掉了三类旧措施（用户原话「你现有的措施全部删掉」）：
  //   · 整页 reload 的 60 秒限流 reloadThrottled；
  //   · 「连第一屏都读不到就 reload」；
  //   · 回前台读失败就重建页面。
  // 它们都在处理「读取失败」的表象，实质什么都没改变——真因是 Go 侧复用了
  // 已被系统回收的 keep-alive 连接（已在 httpx.New 里改成短连接）。
  var dailyInfo = null;   // /api/daily 的结果，提示文案用

  // 前端日志（scope=app）：启动判定、刷新、失败都留痕。
  // 走引擎的日志缓冲，和 Go 侧的请求日志在同一条时间线上，
  // 排查「它为什么没签 / 为什么又签了」时不用两头对时间。
  function logApp(msg, level){
    post('/api/log', { level: level || 'info', scope: 'app', msg: msg }).catch(function(){});
  }

  // roundId：生成本轮的 4 位短 id，和 Go 侧日志的 round 字段同一格式。
  // 只用于把「一次执行」的日志串起来，不追求全局唯一。
  function roundId(){
    var s = '';
    for (var i = 0; i < 4; i++) s += '0123456789abcdef'.charAt(Math.floor(Math.random() * 16));
    return s;
  }

  // markDaily 把这一轮的进度记到引擎侧（落盘）。
  //
  // 三种打点：
  //   begin   一轮开始（Done=false 立刻落盘，中途被杀下次允许补跑）
  //   account 单账号结果（ok / already / fail），用于「今天跑过」时提示失败的那几个
  //   finish  一轮走完（含领奖与抽奖）
  //
  // 全部 fire-and-forget：记录失败绝不能影响签到本身。
  function markDaily(phase, round, row, status){
    var body = { phase: phase, round: round };
    if (row){
      body.provider = row.provider || '';
      body.account = row.account_id || '';
      body.status = status || '';
    }
    post('/api/daily/mark', body).catch(function(){});
  }

  // promptAfterRun：今天已经跑过 → 只刷新，并把「还差什么」说清楚。
  //
  // 提示只列事实：哪几个账号没签成功、哪几个当时失败。不自动重试——
  // 用户明确要求「当天不再自动重试，只在提示里列出来」，
  // 因为重试就是又一次写请求，风险还是自己的。
  function promptAfterRun(){
    var rows = state.rows || [];
    var unsigned = [], failed = [];
    var accts = (dailyInfo && dailyInfo.accounts) || {};
    for (var i = 0; i < rows.length; i++){
      var r = rows[i];
      if (r.error) { failed.push(pname(r) + pshort(r)); continue; }
      if (r.checked_in) continue;
      // 今天跑过一轮但仍是未签：当时失败的单独归一类，其余算「未签」
      if (accts[(r.provider || '') + '|' + (r.account_id || '')] === 'fail'){
        failed.push(pname(r) + pshort(r));
      } else {
        unsigned.push(pname(r) + pshort(r));
      }
    }
    var head = '今日已签到' + (dailyInfo && dailyInfo.at ? '（' + dailyInfo.at + '）' : '') + '，本轮只刷新不重签';
    var parts = [];
    if (unsigned.length) parts.push('未签到：' + unsigned.join('、'));
    if (failed.length) parts.push('失败：' + failed.join('、'));
    if (!parts.length) parts.push('全部账号已签，无待办');
    var text = head + '。' + parts.join('；');
    if (unsigned.length || failed.length){
      actErr(text + '。要补签请点底部「一键签到」', 10000);
    } else {
      actDone(text, 6000);
    }
    logApp('启动判定：今天已跑过一轮 → 只刷新，不重签。' + parts.join('；'));
  }

  // bootCheck：打开 App 时唯一的自动入口。
  //
  // 先问引擎「今天跑过没有」，再二选一：
  //   跑过 → load() 刷新 + 提示（不签、不领、不抽）
  //   没跑 → checkinAll({quiet:true})，和点「一键签到」完全同一条流程
  //
  // 注意：**一键签到按钮的逻辑一个字都没动**。按钮永远真跑一轮，
  // 不看这里的判定——用户明确警告过「别改一键签到按钮的逻辑」。
  function bootCheck(){
    actShow('正在检查今天的签到记录…');
    return j('/api/daily').then(function(d){
      dailyInfo = d || null;
      if (d && d.ran){
        return load(true).then(function(){ promptAfterRun(); });
      }
      return checkinAll({ quiet: true });
    }).catch(function(e){
      // 连判定都拿不到（引擎没起来 / 端口没通）：按「没跑过」处理太危险，
      // 宁可只刷新一次并说明原因，也不要在不确定的情况下自动发写请求。
      logApp('启动判定失败：' + (e && e.message), 'warn');
      actErr('读取今日记录失败：' + ((e && e.message) || '未知错误') + '，已跳过自动签到', 8000);
      return load(true);
    });
  }

  // 回前台：只刷新列表，不签到、不判定、不重建。
  //
  // 0.6.4 把它削到只剩 load()：以前这里有「读不到就重建页面」的兜底，
  // 用户实测「实质什么都没改变」，反而因为重建把页面刷白。
  // 现在读不到就如实显示「读取失败 + 重试按钮」，不猜、不折腾。
  function refreshOnResume(){
    if (state.busy) return;
    load(true);
  }
  window.__pointsAutoCheckin = bootCheck;
  window.__pointsRefresh = refreshOnResume;

  /* ---------- 启动 ---------- */
  // 主题要在首屏渲染前就定好，否则深色用户会先看到一帧白底再翻黑。
  // 这里不再有任何系统偏好分支：localStorage 里是什么就用什么，没有就默认深色。
  applyTheme(readTheme());
  // 视图密度也要在首屏前定好：上次选了紧凑列表，就不要再闪一帧大卡片
  applyView(readView());
  showSkeleton();
  // 打开 App 只跑这一处判定：今天跑过就只刷新 + 提示，没跑过才走完整流程。
  // 这条路径是「自动签到」的唯一入口，壳层不再单独注入签到。
  bootCheck();
  // 0.6.7： meta 原由「设置」页填充，设置页精简后移到这里统一取一次，
  // 保证「日志 → 导出」头部的版本号不退化成「版本 —」。
  j('/api/meta').then(function(m){ state.meta = m || null; }).catch(function(){});
  // 壳层在 WorkBuddy 登录后会 reload 本页，内存变量清零：
  // 从 localStorage 认出「上次是去登录 WorkBuddy」，把换取 token 的流程续上。
  (function(){
    var pend = '';
    try { pend = localStorage.getItem('oauth_pending') || ''; } catch (e) {}
    if (pend === 'workbuddy'){
      try { localStorage.removeItem('oauth_pending'); } catch (e) {}
      finishOAuth('workbuddy');
    }
  })();
  // 从后台回到前台：只刷新列表，不签到、不领奖。
  // 用户明确要求「签到只在打开 App 时做一次」，切后台次数多了会黑号。
  document.addEventListener('visibilitychange', function(){
    if (document.visibilityState !== 'visible') return;
    resumeAfterLogin();
    refreshOnResume();
  });
  // bfcache 恢复（iOS 从登录页返回常见）：pageshow 的 persisted=true 时脚本不会重跑，
  // 所以这里要补一次刷新；但依然不签到。
  window.addEventListener('pageshow', function(e){
    if (e.persisted) { resumeAfterLogin(); refreshOnResume(); }
  });
})();

function ICON(name){
  var P = {
    spark:'<path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z"/><path d="M18.5 15.5l.8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2-2.2-.8 2.2-.8z"/>',
    warn:'<path d="M12 4l9 16H3z"/><path d="M12 10v4.5"/><circle cx="12" cy="17.4" r=".9" fill="currentColor" stroke="none"/>',
    checkAll:'<path d="M1.5 12.5l4.5 4.5 8-9"/><path d="M8.5 16l1.5 1.5 11-12"/>',
    plus:'<path d="M12 5v14"/><path d="M5 12h14"/>',
    arrow:'<path d="M5 12h13"/><path d="M12.5 5.5L19 12l-6.5 6.5"/>',
    refresh:'<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>',
    task:'<rect x="4" y="3.5" width="15" height="15" rx="3"/><path d="M8.5 12l2.5 2.5 5-5.5"/>',
    // 齿轮：外圈八齿 + 中心圆孔（与 icons.go 保持一致）。
    // 不要用「小圆 + 放射短线」——那是太阳的画法，40px 下根本分不出来。
    gear:'<path d="M12 2.6l1.9 1.1 2.2-.3 1.2 1.9 2 1 .2 2.2 1.5 1.6-1.5 1.6-.2 2.2-2 1-1.2 1.9-2.2-.3L12 21.4l-1.9-1.1-2.2.3-1.2-1.9-2-1-.2-2.2L3 12l1.5-1.6.2-2.2 2-1 1.2-1.9 2.2.3z"/><circle cx="12" cy="12" r="3.1"/>',
    // 主题两态：太阳 = 浅色 / 月亮 = 深色（跟随系统已移除）
    sun:'<circle cx="12" cy="12" r="4"/><path d="M12 2.6v2.2M12 19.2v2.2M21.4 12h-2.2M4.8 12H2.6M18.6 5.4l-1.5 1.5M6.9 17.1l-1.5 1.5M18.6 18.6l-1.5-1.5M6.9 6.9L5.4 5.4"/>',
    moon:'<path d="M20.5 14.3A8.6 8.6 0 0 1 9.7 3.5a8.6 8.6 0 1 0 10.8 10.8z"/>',
    // 说明：圆圈里一个小写 i，和「更多」的三点区分明显
    info:'<circle cx="12" cy="12" r="8.6"/><path d="M12 10.6v6"/><circle cx="12" cy="7.6" r="1" fill="currentColor" stroke="none"/>',
    // 视图密度：list = 三行横线（紧凑列表）/ grid = 四格（卡片平铺）
    list:'<path d="M8 6.5h12M8 12h12M8 17.5h12"/><circle cx="4" cy="6.5" r="1.1" fill="currentColor" stroke="none"/><circle cx="4" cy="12" r="1.1" fill="currentColor" stroke="none"/><circle cx="4" cy="17.5" r="1.1" fill="currentColor" stroke="none"/>',
    grid:'<rect x="3.5" y="3.5" width="7" height="7" rx="2"/><rect x="13.5" y="3.5" width="7" height="7" rx="2"/><rect x="3.5" y="13.5" width="7" height="7" rx="2"/><rect x="13.5" y="13.5" width="7" height="7" rx="2"/>',
    // 抽奖：奖池里的票根（齿轮状的太阳/齿轮都不合适，用票根最直观）
    ticket:'<path d="M4 8.5a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v1.6a2 2 0 0 0 0 3.8v1.6a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-1.6a2 2 0 0 0 0-3.8z"/><path d="M13.5 7.5v9" stroke-dasharray="2 2.4"/>',
    trophy:'<path d="M8 4.5h8v4a4 4 0 0 1-8 0z"/><path d="M8 5.5H5.5v1.5A3 3 0 0 0 8 10"/><path d="M16 5.5h2.5v1.5A3 3 0 0 1 16 10"/><path d="M12 12.5v3.5"/><path d="M8.5 19.5h7"/>',
    grip:'<circle cx="9" cy="6" r="1.2" fill="currentColor" stroke="none"/><circle cx="15" cy="6" r="1.2" fill="currentColor" stroke="none"/><circle cx="9" cy="12" r="1.2" fill="currentColor" stroke="none"/><circle cx="15" cy="12" r="1.2" fill="currentColor" stroke="none"/><circle cx="9" cy="18" r="1.2" fill="currentColor" stroke="none"/><circle cx="15" cy="18" r="1.2" fill="currentColor" stroke="none"/>',
    // API 令牌：钥匙。环在左上、齿朝右下，和「盾牌 / 锁」区分开。
    key:'<circle cx="8.5" cy="8.5" r="3.6"/><path d="M11.1 11.1L19 19"/><path d="M16.2 16.2l2-2"/>',
    // 通知：铃铛
    bell:'<path d="M6.5 10a5.5 5.5 0 0 1 11 0c0 4 1.4 5.4 1.9 6.2.2.3 0 .8-.4.8H5c-.4 0-.6-.5-.4-.8.5-.8 1.9-2.2 1.9-6.2z"/><path d="M10 20.2a2.2 2.2 0 0 0 4 0"/>',
    // 复制：两张叠放的纸
    copy:'<rect x="8.5" y="8.5" width="11" height="11" rx="2.6"/><path d="M15.5 8.5v-2a2 2 0 0 0-2-2h-7a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h2"/>'
  };
  return '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' + (P[name] || '') + '</svg>';
}
</script></body></html>`

// indexHTML 由模板 + 图标注塑而成。
//
// 为什么不在 HTML 里内联图标：图标属性（viewBox/线宽）要统一，抽成 iconSVG 一处维护；
// 而 Go 的 const 字符串不支持函数拼接，所以用占位符在启动时注塑一次（几微秒成本）。
var indexHTML = buildIndex()

func buildIndex() string {
	out := indexTmpl
	for _, name := range []string{"spark", "refresh", "checkAll", "plus", "warn", "gear", "close", "link", "clock", "coin", "check", "arrow", "task", "sun", "moon", "info", "list", "grid", "ticket", "trophy"} {
		out = strings.ReplaceAll(out, "{{ICON:"+name+"}}", iconSVG(name))
	}
	return out
}
