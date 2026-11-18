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
<meta name="theme-color" content="#FBF8FF" media="(prefers-color-scheme:light)">
<meta name="theme-color" content="#141218" media="(prefers-color-scheme:dark)">
<title>积分助手</title>
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
  --sat:env(safe-area-inset-top); --sab:env(safe-area-inset-bottom);
}
@media (prefers-color-scheme: dark){
  :root{
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
}
*{box-sizing:border-box;-webkit-tap-highlight-color:transparent}
html{height:100%}
body{
  margin:0;background:var(--surface);color:var(--on-surface);
  font:15px/1.55 -apple-system,BlinkMacSystemFont,"PingFang SC","Noto Sans SC",system-ui,sans-serif;
  -webkit-font-smoothing:antialiased;
  overscroll-behavior-y:contain;   /* 禁掉 WebView 自身的橡皮筋，交给我们自己的下拉刷新 */
  /* 给固定底栏留空间：底栏高度 ≈ 按钮 48 + 上下内边距 20 + 安全区 */
  padding-bottom:calc(84px + var(--sab));
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

/* ---------- 汇总卡 ---------- */
.hero{padding:14px 18px 6px}
.hero .kicker{font-size:12.5px;color:var(--on-surface-v);letter-spacing:.02em;margin-bottom:2px}
.total{
  display:flex;align-items:baseline;gap:8px;
  font-variant-numeric:tabular-nums;
}
.total .num{
  font-size:42px;font-weight:700;letter-spacing:-.035em;line-height:1.06;
  background:linear-gradient(120deg,var(--primary),#8B4DFF 90%);
  -webkit-background-clip:text;background-clip:text;-webkit-text-fill-color:transparent;
}
.total .unit{font-size:14px;color:var(--on-surface-v);font-weight:500}
.chips{display:flex;gap:8px;margin-top:12px;flex-wrap:wrap}
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
.c-top{display:flex;align-items:center;gap:10px;margin-bottom:2px}
.avatar{
  width:38px;height:38px;border-radius:12px;display:grid;place-items:center;
  font-size:15px;font-weight:700;color:#fff;flex:0 0 auto;
  box-shadow:0 3px 10px rgba(0,0,0,.14);
}
.c-name{flex:1;min-width:0}
.c-name .p{font-size:15px;font-weight:640;letter-spacing:-.01em;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.c-name .s{font-size:12.5px;color:var(--on-surface-v);margin-top:1px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.balance{display:flex;align-items:baseline;gap:5px;font-variant-numeric:tabular-nums;
  margin-left:48px;margin-top:-2px}
.balance .n{font-size:24px;font-weight:690;letter-spacing:-.03em}
.balance .u{font-size:12.5px;color:var(--on-surface-v)}
.c-foot{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-top:8px;
  padding-top:10px;border-top:1px solid var(--outline-v)}
.status{display:flex;align-items:center;gap:6px;font-size:12.5px;color:var(--on-surface-v);min-width:0}
.status .txt{white-space:nowrap;overflow:hidden;text-overflow:ellipsis;flex:1;min-width:0}
.status.ok{color:var(--ok)}.status.bad{color:var(--bad)}
.status svg{width:15px;height:15px}
.pill{
  border:0;border-radius:var(--r-pill);padding:9px 18px;font-size:13.5px;font-weight:640;
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

/* ---------- 底部操作条 ---------- */
.bar{
  position:fixed;left:0;right:0;bottom:0;z-index:28;
  padding:10px 14px calc(10px + var(--sab));
  background:color-mix(in srgb,var(--surface) 90%,transparent);
  backdrop-filter:saturate(180%) blur(18px);-webkit-backdrop-filter:saturate(180%) blur(18px);
  border-top:1px solid var(--outline-v);
  display:flex;gap:10px;
}
.btn{
  flex:1;border:0;border-radius:var(--r-pill);padding:14px 16px;
  font-size:15px;font-weight:650;letter-spacing:.01em;
  display:inline-flex;align-items:center;justify-content:center;gap:8px;
  transition:transform .14s,opacity .14s;
}
.btn:active{transform:scale(.975)}
.btn[disabled]{opacity:.45}
.btn.tonal{background:var(--surface-2);color:var(--on-surface)}
/* 领任务按钮：青绿色，与「签到」在视觉上区分任务/签到两类动作 */
.btn.task{background:var(--ok-c);color:var(--ok)}
.btn.filled{background:var(--primary);color:var(--on-primary);box-shadow:0 6px 18px color-mix(in srgb,var(--primary) 32%,transparent)}
.btn svg{width:19px;height:19px}

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
.sect-h{font-size:13px;font-weight:680;color:var(--on-surface-v);
  margin:18px 4px 10px;letter-spacing:.02em}
.sect-h:first-child{margin-top:6px}
.notice{
  background:var(--warn-c);color:var(--warn);border-radius:var(--r-md);
  padding:10px 12px;font-size:12.5px;line-height:1.55;margin-bottom:12px;
}
.meta{font-size:12px;color:var(--on-surface-v);text-align:center;padding:14px 0 4px}
/* 卡片右上角的管理入口 */
.c-more{
  border:0;background:transparent;color:var(--on-surface-v);
  width:32px;height:32px;border-radius:50%;display:grid;place-items:center;flex:0 0 auto;
}
.c-more:active{background:var(--surface-2)}
.c-more svg{width:18px;height:18px}

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
    <h1>积分助手</h1>
  </div>
  <button class="iconbtn" id="btnRefresh" aria-label="刷新">{{ICON:refresh}}</button>
  <button class="iconbtn" id="btnSettings" aria-label="设置">{{ICON:gear}}</button>
</div>

<div class="hero">
  <div class="kicker">全部账号合计</div>
  <div class="total"><span class="num" id="total">—</span><span class="unit" id="total-unit">积分</span></div>
  <div class="chips" id="chips"></div>
</div>

<div class="list" id="list"></div>

<div class="bar">
  <button class="btn tonal" id="btnAll" disabled>{{ICON:checkAll}}一键签到</button>
  <button class="btn task" id="btnTasks" disabled>{{ICON:task}}领任务</button>
  <button class="btn filled" id="btnAdd">{{ICON:plus}}添加账号</button>
</div>

<div class="snack" id="snack"></div>

<script>
(function(){
  "use strict";
  var $ = function(id){ return document.getElementById(id); };
  var state = { rows: [], busy: false, providers: [] };

  /* ---------- 工具 ---------- */
  function fmt(n){
    n = Number(n) || 0;
    // 整数不显示小数，避免「1,500.00 积分」这种冗余
    return n.toLocaleString('zh-CN', { maximumFractionDigits: n % 1 ? 1 : 0 });
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
    // 关键：iOS 进入后台后，本机回环连接的 socket 可能被系统挂起/复用失效，
    // 回到前台第一次 fetch 会直接失败。这里对「网络层失败」自动重试一次，
    // 第二次往往能拿到新连接，避免「读取失败 + 下拉刷新也救不回来」的死局。
    var attempt = function(tries){
      var ctrl = (typeof AbortController !== 'undefined') ? new AbortController() : null;
      var opt = o || {};
      var timer = setTimeout(function(){ if (ctrl) ctrl.abort(); }, 25000);
      if (ctrl) opt.signal = ctrl.signal;
      return fetch(u, opt).then(function(r){
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return r.json();
      }).catch(function(e){
        if (e && e.name === 'AbortError') throw new Error('请求超时，请检查网络后下拉刷新');
        if (tries < 1 && (e instanceof TypeError || /fetch|network|failed/i.test(String(e.message)))){
          // 连接层失败（不是业务错误/超时）：稍等片刻重试一次
          return new Promise(function(resolve){ setTimeout(resolve, 500); }).then(function(){
            return attempt(tries + 1);
          });
        }
        throw e;
      }).then(function(v){ clearTimeout(timer); return v; }, function(e){ clearTimeout(timer); throw e; });
    };
    return attempt(0);
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
  function openSheet(title, subtitle, build){
    var mask = el('div', 'mask');
    var sheet = el('div', 'sheet');
    sheet.appendChild(el('div', 'sheet-h', title));
    if (subtitle) sheet.appendChild(el('div', 'sheet-sub', subtitle));
    var body = el('div', '');
    sheet.appendChild(body);
    var cancel = el('button', 'sheet-cancel', '关闭');
    sheet.appendChild(cancel);
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
    cancel.addEventListener('click', close);
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

  function cardOf(r){
    var card = document.createElement('div');
    card.className = 'card' + (r.error ? ' err' : '');

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
    // 卡片右上角的管理入口：用户明确要求「删除入口」，不能藏在设置里
    var more = document.createElement('button');
    more.className = 'c-more';
    more.setAttribute('aria-label', '账号管理');
    more.innerHTML = ICON('more');
    more.addEventListener('click', function(e){
      e.stopPropagation();
      openAccountMenu(r);
    });
    top.appendChild(more);

    var bal = document.createElement('div'); bal.className = 'balance';
    var n = document.createElement('span'); n.className = 'n';
    n.textContent = r.error ? '—' : fmt(r.balance);
    var u = document.createElement('span'); u.className = 'u'; u.textContent = r.unit || '积分';
    bal.appendChild(n); bal.appendChild(u);
    card.appendChild(top); card.appendChild(bal);

    var foot = document.createElement('div'); foot.className = 'c-foot';
    var st = document.createElement('div');
    st.className = 'status ' + (r.error ? 'bad' : (r.checked_in ? 'ok' : ''));
    var stTxt = document.createElement('span'); stTxt.className = 'txt';
    if (r.error) stTxt.textContent = r.error;
    else if (r.detail) stTxt.textContent = r.detail;
    else stTxt.textContent = r.checked_in ? '今天已签到' : '今天还没签';
    st.appendChild(stTxt);

    // 右侧按钮组：任务（仅支持平台）+ 签到
    var btns = document.createElement('div');
    btns.className = 'c-actions';

    if (r.has_tasks && !r.error){
      var taskBtn = document.createElement('button');
      taskBtn.className = 'pill task';
      taskBtn.textContent = '任务';
      taskBtn.setAttribute('aria-label', '查看 ' + (r.provider_name || r.provider || '') + ' 每日任务');
      taskBtn.addEventListener('click', function(){
        openTasks(r);
      });
      btns.appendChild(taskBtn);
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

  function render(){
    var rows = state.rows;
    var list = $('list');
    var total = 0, healthy = 0, unit = '';
    for (var i = 0; i < rows.length; i++){
      if (!rows[i].error){
        total += Number(rows[i].balance) || 0;
        healthy++;
        // 各家单位不同（积分 / 点数）时不能相加，改用中性文案
        var u = rows[i].unit || '积分';
        if (unit && unit !== u) unit = '合计'; else unit = u;
      }
    }
    // 全异常或没有账号时显示「—」而不是 0：0 会被误读成「余额为零」。
    // 多家单位不同（灵石 / 积分）时也不能相加，直接显示「—」，否则 100 灵石 +
    // 12.5 积分 会变成一个没有任何含义的 112.5。
    var mixed = (unit === '合计');
    $('total').textContent = (!rows.length || !healthy || mixed) ? '—' : fmt(total);
    var tu = $('total-unit');
    if (tu) tu.textContent = mixed ? '' : (unit || '积分');
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
      $('btnTasks').disabled = true;
      return;
    }

    // 一次插入，避免逐条 append 引起多次重排
    var frag = document.createDocumentFragment();
    for (var k = 0; k < rows.length; k++) frag.appendChild(cardOf(rows[k]));
    list.textContent = '';
    list.appendChild(frag);
    $('btnAll').disabled = state.busy;
    // 「领任务」只在存在支持任务体系的账号时可点
    var hasTaskAcct = false;
    for (var m = 0; m < rows.length; m++){
      if (rows[m].has_tasks && !rows[m].error){ hasTaskAcct = true; break; }
    }
    $('btnTasks').disabled = state.busy || !hasTaskAcct;
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
      }
    }
    return rows;
  }

  function load(silent){
    if (!silent) showSkeleton();
    return j('/api/providers').then(function(ps){
      state.providers = Array.isArray(ps) ? ps : [];
      return Promise.all([j('/api/query'), j('/api/accounts')]);
    }).then(function(both){
      var rows = Array.isArray(both[0]) ? both[0] : [];
      var accts = Array.isArray(both[1]) ? both[1] : [];
      var enabled = {};
      for (var i = 0; i < accts.length; i++) enabled[accts[i].account_id] = accts[i].enabled;
      for (var k = 0; k < rows.length; k++){
        if (enabled.hasOwnProperty(rows[k].account_id)) rows[k].enabled = enabled[rows[k].account_id];
      }
      state.rows = enrich(rows);
      render();
      // 返回成功标志：调用方（自动签到）据此判断是否需要 reload 兜底
      return true;
    }).catch(function(e){
      // 失败态给出可点击的重试，而不是一行"读取失败"让用户卡死
      var list = $('list');
      list.innerHTML =
        '<div class="empty"><div class="ring">' + ICON('warn') + '</div>' +
        '<h2>读取失败</h2><p>' + esc(e.message) + '</p>' +
        '<button class="btn filled" id="retryBtn" style="max-width:220px">重新加载</button></div>';
      var rb = $('retryBtn');
      if (rb) rb.addEventListener('click', function(){ load(false); });
      // 返回失败标志而不是抛出：下拉刷新/按钮刷新等调用点不用各自接异常，
      // 自动签到则靠这个标志决定是否整页 reload 兜底。
      return false;
    });
  }

  function doCheckin(id, btn){
    if (btn){ btn.disabled = true; btn.textContent = '…'; }
    return j('/api/checkin?account=' + encodeURIComponent(id), { method: 'POST' })
      .then(function(res){
        var r = Array.isArray(res) ? res[0] : res;
        if (r && r.error){ toast(r.error); }
        else if (r && r.already){ toast('今天已经签过了'); }
        else if (r && r.ok){ toast('签到成功' + (r.gain ? '，+' + fmt(r.gain) + ' ' + (r.unit || '积分') : '')); }
        else { toast('签到未成功'); }
        return load(true);
      })
      .catch(function(e){ toast('签到失败：' + e.message); if (btn) btn.disabled = false; });
  }

  function checkinAll(){
    if (state.busy) return;
    state.busy = true;
    var b = $('btnAll'); b.disabled = true; b.textContent = '签到中…';
    j('/api/checkin', { method: 'POST' })
      .then(function(res){
        var arr = Array.isArray(res) ? res : [res];
        var ok = 0, skip = 0, bad = 0;
        for (var i = 0; i < arr.length; i++){
          if (arr[i].error) bad++; else if (arr[i].already) skip++; else ok++;
        }
        toast('完成：新签 ' + ok + '，已签 ' + skip + (bad ? '，失败 ' + bad : ''));
        return load(true);
      })
      .catch(function(e){ toast('签到失败：' + e.message); })
      .then(function(){
        state.busy = false;
        b.disabled = false; b.innerHTML = ICON('checkAll') + '一键签到';
      });
  }

  function claimAllTasks(){
    if (state.busy) return;
    state.busy = true;
    var b = $('btnTasks'); b.disabled = true; b.textContent = '领取中…';
    j('/api/tasks/claim_all', { method: 'POST' })
      .then(function(res){
        var rows = (res && res.results) || [];
        var claimed = 0, gain = 0, unit = '', bad = 0;
        for (var i = 0; i < rows.length; i++){
          claimed += rows[i].claimed || 0;
          gain += rows[i].gain || 0;
          if (rows[i].unit) unit = rows[i].unit;
          if (rows[i].error) bad++;
        }
        var msg = claimed > 0
          ? '已领 ' + claimed + ' 个任务' + (gain > 0 ? '，+' + fmt(gain) + ' ' + (unit || '') : '')
          : '没有可领取的任务';
        if (bad) msg += '，' + bad + ' 个账号有失败';
        toast(msg);
        return load(true);
      })
      .catch(function(e){ toast('领取失败：' + e.message); })
      .then(function(){
        state.busy = false;
        b.disabled = false; b.innerHTML = ICON('task') + '领任务';
      });
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
  $('btnTasks').addEventListener('click', claimAllTasks);
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
      // 不依赖平台列表、也不依赖网页登录的兜底入口
      var manual = el('button', 'sheet-cancel', '直接手动粘贴凭据 / 导入备份');
      manual.addEventListener('click', function(){
        close();
        setTimeout(function(){ openSettings(); }, 260);
      });
      body.appendChild(manual);
    });
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

  /* ---------- 设置：账号管理 / 备份 / 版本 ---------- */
  function openSettings(){
    openSheet('设置', '账号管理、备份导出与导入。', function(body, close){
      body.appendChild(el('div', 'sect-h', '账号管理'));
      var accBox = el('div', '');
      body.appendChild(accBox);

      function refreshAccounts(){
        j('/api/accounts').then(function(list){
          accBox.textContent = '';
          if (!Array.isArray(list) || !list.length){
            accBox.appendChild(el('div', 'sheet-sub', '还没有账号，点底部「添加账号」。'));
            return;
          }
          list.forEach(function(a){
            var row = el('div', 'acc-row' + (a.enabled ? '' : ' off'));
            var dot = el('span', 'dot');
            var h = hueOf(a.provider);
            dot.style.background = 'linear-gradient(135deg,hsl(' + h + ',72%,58%),hsl(' + ((h + 34) % 360) + ',72%,50%))';
            dot.textContent = initials(platformName(a.provider));
            var info = el('div', 'info');
            info.appendChild(el('b', '', platformName(a.provider) + (a.label ? ' · ' + a.label : '')));
            info.appendChild(el('i', '', a.account_id));
            row.appendChild(dot); row.appendChild(info);

            var btnToggle = el('button', 'mini', a.enabled ? '停用' : '启用');
            btnToggle.addEventListener('click', function(){
              post('/api/account/toggle', {
                provider: a.provider, account_id: a.account_id, enabled: !a.enabled
              }).then(function(r){
                if (!r.ok){ toast(r.error || '操作失败'); return; }
                refreshAccounts(); load(true);
              }).catch(function(e){ toast('操作失败：' + e.message); });
            });
            row.appendChild(btnToggle);

            // 二次点击确认：WebView 里 window.confirm 常被吞掉，不能依赖它
            var armed = false, resetTimer;
            var btnDel = el('button', 'mini', '删除');
            btnDel.addEventListener('click', function(){
              if (!armed){
                armed = true; btnDel.textContent = '确认删除'; btnDel.classList.add('del');
                resetTimer = setTimeout(function(){
                  armed = false; btnDel.textContent = '删除'; btnDel.classList.remove('del');
                }, 4000);
                return;
              }
              clearTimeout(resetTimer);
              post('/api/account/delete', { provider: a.provider, account_id: a.account_id })
                .then(function(r){
                  if (!r.ok){ toast(r.error || '删除失败'); return; }
                  toast('已删除账号'); refreshAccounts(); load(true);
                }).catch(function(e){ toast('删除失败：' + e.message); });
            });
            row.appendChild(btnDel);
            accBox.appendChild(row);
          });
        }).catch(function(e){
          accBox.appendChild(el('div', 'sheet-sub', '读取账号失败：' + e.message));
        });
      }
      refreshAccounts();

      // 手动导入入口（设置页也能直接进）
      var quick = el('div', 'row-btns');
      var qAdd = el('button', 'btn ghost', '手动添加凭据');
      qAdd.addEventListener('click', function(){
        close(); setTimeout(function(){ openManualSheet(''); }, 260);
      });
      quick.appendChild(qAdd);
      body.appendChild(quick);

      body.appendChild(el('div', 'sect-h', '备份导出'));
      var notice = el('div', 'notice', '备份里是明文 Cookie / Token，只存本机。别发到聊天、网盘或任何第三方。');
      body.appendChild(notice);
      var fExp = mkField('备份内容（可长按全选复制）', 'textarea', {
        readonly: 'readonly', rows: '6', class: 'mono', placeholder: '点下面的「生成备份」'
      });
      // class 属性用 setAttribute 会覆盖 className，这里显式设置
      fExp.input.className = 'mono';
      body.appendChild(fExp.wrap);
      var expBtns = el('div', 'row-btns');
      var btnExp = el('button', 'btn ghost', '生成备份');
      var btnCopy = el('button', 'btn ghost', '复制');
      expBtns.appendChild(btnExp); expBtns.appendChild(btnCopy);
      body.appendChild(expBtns);
      btnExp.addEventListener('click', function(){
        j('/api/export').then(function(d){
          fExp.input.value = JSON.stringify(d, null, 2);
          toast('备份已生成');
        }).catch(function(e){ toast('导出失败：' + e.message); });
      });
      btnCopy.addEventListener('click', function(){ copyText(fExp.input); });

      body.appendChild(el('div', 'sect-h', '备份导入'));
      var fImp = mkField('把备份 JSON 粘贴到这里', 'textarea', {
        rows: '6', placeholder: '{"version":1,"accounts":[...]}'
      });
      fImp.input.className = 'mono';
      body.appendChild(fImp.wrap);
      var btnImp = el('button', 'btn filled', '导入备份');
      btnImp.style.width = '100%';
      btnImp.addEventListener('click', function(){
        var txt = fImp.input.value.trim();
        if (!txt){ toast('先粘贴备份内容'); return; }
        btnImp.disabled = true;
        post('/api/import', { text: txt }).then(function(r){
          btnImp.disabled = false;
          if (!r.ok){ toast('导入失败：' + (r.error || '未知错误')); return; }
          toast('导入成功：新增 ' + r.added + (r.skipped ? '，跳过 ' + r.skipped : ''));
          refreshAccounts(); load(true);
        }).catch(function(e){
          btnImp.disabled = false; toast('导入失败：' + e.message);
        });
      });
      body.appendChild(btnImp);

      var meta = el('div', 'meta', '版本 —');
      body.appendChild(meta);
      j('/api/meta').then(function(m){
        meta.textContent = '版本 ' + (m.version || '—') + ' · 已支持 ' + (m.providers || 0) + ' 个平台';
      }).catch(function(){});
    });
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
                    toast('领取成功' + (res && res.gain ? '，+' + fmt(res.gain) + ' ' + (res.unit || '灵石') : ''));
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

  /* ---------- 卡片右上角：单账号操作 ---------- */
  function openAccountMenu(r){
    openSheet(platformName(r.provider), r.label || r.account_id || '', function(body, close){
      var info = el('div', 'sheet-sub', '账号 ID：' + (r.account_id || '—'));
      body.appendChild(info);
      var toggle = el('button', 'sheet-item');
      var tt = el('span', 'sheet-txt');
      tt.appendChild(el('b', '', r.enabled === false ? '启用此账号' : '停用此账号'));
      tt.appendChild(el('i', '', '停用后不参与查询与签到，凭据保留'));
      toggle.appendChild(tt);
      toggle.addEventListener('click', function(){
        post('/api/account/toggle', {
          provider: r.provider, account_id: r.account_id, enabled: r.enabled === false
        }).then(function(res){
          if (!res.ok){ toast(res.error || '操作失败'); return; }
          toast('已更新'); close(); load(true);
        }).catch(function(e){ toast('操作失败：' + e.message); });
      });
      body.appendChild(toggle);

      var del = el('button', 'sheet-item');
      var dt = el('span', 'sheet-txt');
      dt.appendChild(el('b', '', '删除账号'));
      dt.appendChild(el('i', '', '会同时删掉保存的凭据，需要再点一次确认'));
      del.appendChild(dt);
      var armed = false;
      del.addEventListener('click', function(){
        if (!armed){
          armed = true; dt.firstChild.textContent = '再点一次确认删除';
          setTimeout(function(){ armed = false; dt.firstChild.textContent = '删除账号'; }, 4000);
          return;
        }
        post('/api/account/delete', { provider: r.provider, account_id: r.account_id })
          .then(function(res){
            if (!res.ok){ toast(res.error || '删除失败'); return; }
            toast('已删除账号'); close(); load(true);
          }).catch(function(e){ toast('删除失败：' + e.message); });
      });
      body.appendChild(del);
    });
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
  // 自动签到：壳层在「每次打开 App / 回到前台」调用一次。
  // 只对未签账号发起签到，已签的完全不发请求（省流量、降低风控概率）。
  // 幂等：并发调用直接返回同一个 Promise，避免 Android onResume 与页面
  // visibilitychange 同时触发时发两轮请求（重复签到会被平台判为异常）。
  var autoTask = null;
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
  function autoCheckin(allowReload){
    if (autoTask) return autoTask;
    autoTask = load(true).then(function(ok){
      if (!ok){
        // iOS 后台回前台：WKWebView 的网络进程可能被系统回收，之后所有 fetch
        // 都会持续失败，重试也救不回来。此时唯一出路是整页 reload，让 WebKit
        // 重建网络栈。先写恢复标志，新页面加载后据此把自动签到续上。
        if (allowReload){
          try { localStorage.setItem('resume_checkin', '1'); } catch (err) {}
          notifyReload();
        }
        return false;
      }
      var need = false;
      for (var i = 0; i < state.rows.length; i++){
        var r = state.rows[i];
        if (!r.error && !r.checked_in){ need = true; break; }
      }
      if (need && !state.busy){
        checkinAll();
        return true;
      }
      return need;
    }).then(function(v){
      // 留一点冷却，避免刚结束又被下一次唤醒立刻触发
      setTimeout(function(){ autoTask = null; }, 3000);
      return v;
    });
    return autoTask;
  }
  // 壳层调用（打开 App / 回到前台）默认允许失败时整页重载；
  // 页面自身的 visibilitychange 同理；只有 reload 后的续跑（resumeCheckin）
  // 传 false，避免「失败 → reload → 又失败 → 又 reload」的死循环。
  window.__pointsAutoCheckin = function(){ return autoCheckin(true); };

  // notifyReload：通知壳层重建页面。优先走 PointsBridge（Android 已实现），
  // 没有桥时直接 location.reload() 兜底——iOS WKWebView 里整页 reload
  // 同样会让 WebKit 重建网络连接。
  function notifyReload(){
    try {
      if (window.PointsBridge && typeof PointsBridge.reload === 'function'){
        PointsBridge.reload();
        return;
      }
    } catch (e) {}
    try { location.reload(); } catch (e) {}
  }

  /* ---------- 启动 ---------- */
  showSkeleton();
  load(true);
  // reload 续跑：iOS 后台回前台时网络进程被回收，fetch 持续失败 → autoCheckin
  // 写下 resume_checkin 标志后整页 reload。这里把上次没做完的自动签到续上。
  // 传 false：这次如果再失败就只显示错误，绝不再 reload（防死循环）。
  (function(){
    var resume = '';
    try { resume = localStorage.getItem('resume_checkin') || ''; } catch (e) {}
    if (resume === '1'){
      try { localStorage.removeItem('resume_checkin'); } catch (e) {}
      setTimeout(function(){ autoCheckin(false); }, 800);
    }
  })();
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
  // 页面从后台回到前台时自动刷新 + 自动签到（用户「打开软件即签到」的核心诉求）
  document.addEventListener('visibilitychange', function(){
    if (document.visibilityState !== 'visible') return;
    resumeAfterLogin();
    autoCheckin(true);
  });
  // bfcache 恢复（iOS 从登录页返回常见）：pageshow 的 persisted=true 时脚本不会重跑
  window.addEventListener('pageshow', function(e){
    if (e.persisted) { resumeAfterLogin(); autoCheckin(true); }
  });
})();

function ICON(name){
  var P = {
    spark:'<path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z"/><path d="M18.5 15.5l.8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2-2.2-.8 2.2-.8z"/>',
    warn:'<path d="M12 4l9 16H3z"/><path d="M12 10v4.5"/><circle cx="12" cy="17.4" r=".9" fill="currentColor" stroke="none"/>',
    checkAll:'<path d="M1.5 12.5l4.5 4.5 8-9"/><path d="M8.5 16l1.5 1.5 11-12"/>',
    plus:'<path d="M12 5v14"/><path d="M5 12h14"/>',
    arrow:'<path d="M5 12h13"/><path d="M12.5 5.5L19 12l-6.5 6.5"/>',
    more:'<circle cx="5" cy="12" r="1.5" fill="currentColor" stroke="none"/>' +
         '<circle cx="12" cy="12" r="1.5" fill="currentColor" stroke="none"/>' +
         '<circle cx="19" cy="12" r="1.5" fill="currentColor" stroke="none"/>',
    refresh:'<path d="M20 11a8 8 0 1 0-2.3 5.7"/><path d="M20 5v6h-6"/>',
    task:'<rect x="4" y="3.5" width="15" height="15" rx="3"/><path d="M8.5 12l2.5 2.5 5-5.5"/>',
    gear:'<circle cx="12" cy="12" r="3.2"/><path d="M12 2.5v2.2M12 19.3v2.2M21.5 12h-2.2M4.7 12H2.5M18.7 5.3l-1.6 1.6M6.9 17.1l-1.6 1.6M18.7 18.7l-1.6-1.6M6.9 6.9L5.3 5.3"/>'
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
	for _, name := range []string{"spark", "refresh", "checkAll", "plus", "warn", "gear", "close", "link", "clock", "coin", "check", "arrow", "task"} {
		out = strings.ReplaceAll(out, "{{ICON:"+name+"}}", iconSVG(name))
	}
	return out
}
