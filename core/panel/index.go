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
</div>

<div class="hero">
  <div class="kicker">全部账号合计</div>
  <div class="total"><span class="num" id="total">—</span><span class="unit" id="total-unit">积分</span></div>
  <div class="chips" id="chips"></div>
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
    var ctrl = (typeof AbortController !== 'undefined') ? new AbortController() : null;
    var opt = o || {};
    var timer = setTimeout(function(){ if (ctrl) ctrl.abort(); }, 25000);
    if (ctrl) opt.signal = ctrl.signal;
    return fetch(u, opt).then(function(r){
      if (!r.ok) throw new Error('HTTP ' + r.status);
      return r.json();
    }).catch(function(e){
      if (e && (e.name === 'AbortError')) throw new Error('请求超时，请检查网络后下拉刷新');
      throw e;
    }).then(function(v){ clearTimeout(timer); return v; }, function(e){ clearTimeout(timer); throw e; });
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

    var btn = document.createElement('button');
    btn.className = 'pill' + (r.error ? ' fail' : (r.checked_in ? ' done' : ''));
    btn.setAttribute('aria-label', (r.error ? '重试 ' : (r.checked_in ? '已签到 ' : '签到 ')) + (r.provider_name || r.provider || ''));
    btn.textContent = r.error ? '重试' : (r.checked_in ? '已签' : '签到');
    if (r.checked_in) btn.disabled = true;
    btn.addEventListener('click', function(){
      doCheckin(r.account_id, btn);
    });

    foot.appendChild(st); foot.appendChild(btn);
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
    // 全异常或没有账号时显示「—」而不是 0：0 会被误读成「余额为零」
    $('total').textContent = (!rows.length || !healthy) ? '—' : fmt(total);
    var tu = $('total-unit');
    if (tu) tu.textContent = (unit === '合计') ? '' : (unit || '积分');
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
    var frag = document.createDocumentFragment();
    for (var k = 0; k < rows.length; k++) frag.appendChild(cardOf(rows[k]));
    list.textContent = '';
    list.appendChild(frag);
    $('btnAll').disabled = state.busy;
  }

  /* ---------- 数据 ---------- */
  function enrich(rows){
    var byId = {};
    for (var i = 0; i < state.providers.length; i++) byId[state.providers[i].id] = state.providers[i];
    for (var j = 0; j < rows.length; j++){
      var p = byId[rows[j].provider];
      if (p) rows[j].provider_name = p.name;
    }
    return rows;
  }

  function load(silent){
    if (!silent) showSkeleton();
    return j('/api/providers').then(function(ps){
      state.providers = Array.isArray(ps) ? ps : [];
      return j('/api/query');
    }).then(function(rows){
      state.rows = enrich(Array.isArray(rows) ? rows : []);
      render();
    }).catch(function(e){
      $('list').innerHTML =
        '<div class="empty"><div class="ring">' + ICON('warn') + '</div>' +
        '<h2>读取失败</h2><p>' + esc(e.message) + '</p></div>';
    });
  }

  function doCheckin(id, btn){
    if (btn){ btn.disabled = true; btn.textContent = '…'; }
    return j('/api/checkin?account=' + encodeURIComponent(id), { method: 'POST' })
      .then(function(res){
        var r = Array.isArray(res) ? res[0] : res;
        if (r && r.error){ toast(r.error); }
        else if (r && r.already){ toast('今天已经签过了'); }
        else if (r && r.ok){ toast('签到成功' + (r.gain ? '，+' + fmt(r.gain) + ' 积分' : '')); }
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
  $('btnAdd').addEventListener('click', openAddSheet);

  /* ---------- 添加账号：底部弹层选平台 ---------- */
  function openAddSheet(){
    var mask = document.createElement('div');
    mask.className = 'mask';
    var sheet = document.createElement('div');
    sheet.className = 'sheet';
    var head = document.createElement('div');
    head.className = 'sheet-h';
    head.textContent = '用官方网页登录';
    var hint = document.createElement('div');
    hint.className = 'sheet-sub';
    hint.textContent = '选一个平台，会在内置浏览器里打开它的官网。登录完返回即可。';
    sheet.appendChild(head); sheet.appendChild(hint);

    var ps = state.providers.length ? state.providers : [];
    if (!ps.length){
      var e = document.createElement('div');
      e.className = 'sheet-sub';
      e.textContent = '平台列表加载中，请稍后重试。';
      sheet.appendChild(e);
    }
    // 品牌色：主平台用各自的识别色，其余按 id 稳定取色。
    // 图标只做色块 + 首字，不复制官方 logo（避免商标问题），但颜色能让人一眼分辨。
    var BRAND = {
      workbuddy: ['#5B6CFF', '#7B5CFF'],
      aipy: ['#FF7A3D', '#FF5A5F'],
      dumate: ['#2F6BFF', '#4C8DFF'],
      lingxi: ['#12B886', '#0CA678'],
      trae: ['#FF4D6D', '#E63E8B']
    };
    ps.forEach(function(p){
      var row = document.createElement('button');
      row.className = 'sheet-item';
      var dot = document.createElement('span');
      dot.className = 'sheet-dot';
      var b = BRAND[p.id];
      if (b){
        dot.style.background = 'linear-gradient(135deg,' + b[0] + ',' + b[1] + ')';
      } else {
        var hue = hueOf(p.id);
        dot.style.background = 'linear-gradient(135deg,hsl(' + hue + ',72%,58%),hsl(' + ((hue + 34) % 360) + ',72%,50%))';
      }
      dot.textContent = initials(p.name || p.id);
      var txt = document.createElement('span');
      txt.className = 'sheet-txt';
      txt.innerHTML = '<b>' + esc(p.name || p.id) + '</b><i>' + esc(p.note || '') + '</i>';
      var ar = document.createElement('span');
      ar.className = 'sheet-arw';
      ar.innerHTML = ICON('arrow');
      row.appendChild(dot); row.appendChild(txt); row.appendChild(ar);
      row.addEventListener('click', function(){
        dismiss();
        startLogin(p.id);
      });
      sheet.appendChild(row);
    });

    var cancel = document.createElement('button');
    cancel.className = 'sheet-cancel';
    cancel.textContent = '取消';
    cancel.addEventListener('click', dismiss);
    sheet.appendChild(cancel);

    document.body.appendChild(mask);
    document.body.appendChild(sheet);
    requestAnimationFrame(function(){
      mask.classList.add('on'); sheet.classList.add('on');
    });
    mask.addEventListener('click', dismiss);

    function dismiss(){
      mask.classList.remove('on'); sheet.classList.remove('on');
      setTimeout(function(){
        if (mask.parentNode) mask.parentNode.removeChild(mask);
        if (sheet.parentNode) sheet.parentNode.removeChild(sheet);
      }, 240);
    }
  }

  // 让壳层打开官方登录页。
  // Android: WebView.addJavascriptInterface 注入 window.PointsBridge.openLogin(id)
  // iOS:     WKScriptMessageHandler 同名；都不可用时退回自定义 scheme
  function startLogin(providerId){
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

    // 壳层未必会 reload（例如 iOS 只 dismiss 了登录页）。登录回到前台时若
    // 既不 reload 也不发信号，页面就永远看不到新账号——这是「加完账号没反应」的根因。
    // 这里记一个时间戳，配合 pageshow / visibilitychange 做兜底重查。
    loginPendingAt = Date.now();
  }

  /* ---------- 对外暴露给壳层：打开即签到 ---------- */
  // 自动签到：壳层在「每次打开 App / 回到前台」调用一次。
  // 只对未签账号发起签到，已签的完全不发请求（省流量、降低风控概率）。
  // 幂等：并发调用直接返回同一个 Promise，避免 Android onResume 与页面
  // visibilitychange 同时触发时发两轮请求（重复签到会被平台判为异常）。
  var autoTask = null;
  var loginPendingAt = 0;

  // resumeAfterLogin：登录容器关闭后回到前台时调用。
  // 只要距上次发起登录不到 10 分钟，就重新拉一次数据（新增账号会立刻出现）。
  function resumeAfterLogin(){
    if (!loginPendingAt) return;
    if (Date.now() - loginPendingAt > 10 * 60 * 1000) { loginPendingAt = 0; return; }
    loginPendingAt = 0;
    load(true).then(function(){ toast('已同步账号'); });
  }
  function autoCheckin(){
    if (autoTask) return autoTask;
    autoTask = load(true).then(function(){
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
    }).catch(function(){ return false; }).then(function(v){
      // 留一点冷却，避免刚结束又被下一次唤醒立刻触发
      setTimeout(function(){ autoTask = null; }, 3000);
      return v;
    });
    return autoTask;
  }
  window.__pointsAutoCheckin = autoCheckin;

  /* ---------- 启动 ---------- */
  showSkeleton();
  load(true);
  // 页面从后台回到前台时自动刷新 + 自动签到（用户「打开软件即签到」的核心诉求）
  document.addEventListener('visibilitychange', function(){
    if (document.visibilityState !== 'visible') return;
    resumeAfterLogin();
    autoCheckin();
  });
  // bfcache 恢复（iOS 从登录页返回常见）：pageshow 的 persisted=true 时脚本不会重跑
  window.addEventListener('pageshow', function(e){
    if (e.persisted) { resumeAfterLogin(); autoCheckin(); }
  });
})();

function ICON(name){
  var P = {
    spark:'<path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z"/><path d="M18.5 15.5l.8 2.2 2.2.8-2.2.8-.8 2.2-.8-2.2-2.2-.8 2.2-.8z"/>',
    warn:'<path d="M12 4l9 16H3z"/><path d="M12 10v4.5"/><circle cx="12" cy="17.4" r=".9" fill="currentColor" stroke="none"/>',
    checkAll:'<path d="M1.5 12.5l4.5 4.5 8-9"/><path d="M8.5 16l1.5 1.5 11-12"/>',
    plus:'<path d="M12 5v14"/><path d="M5 12h14"/>'
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
	for _, name := range []string{"spark", "refresh", "checkAll", "plus", "warn", "gear", "close", "link", "clock", "coin", "check", "arrow"} {
		out = strings.ReplaceAll(out, "{{ICON:"+name+"}}", iconSVG(name))
	}
	return out
}
