<div align="center">

# 积分矩阵 · Pointex

**一次点击，把所有平台的签到、任务奖励、抽奖机会全部做完。**

<p>
  <img alt="version" src="https://img.shields.io/badge/version-0.7.2-4F5BD5">
  <img alt="platform" src="https://img.shields.io/badge/platform-iOS%2015%2B%20%7C%20Android%208%2B-2ea44f">
  <img alt="go" src="https://img.shields.io/badge/Go-1.21%2B-00ADD8">
  <img alt="license" src="https://img.shields.io/badge/license-MIT-blue">
</p>

<p>
  <a href="https://github.com/ElyYinLi/aipoints-mobile/releases/tag/android-latest">📥 下载 Android</a>
  ·
  <a href="https://github.com/ElyYinLi/aipoints-mobile/releases/tag/ios-latest">📥 下载 iOS</a>
  ·
  <a href="#本地运行">本地运行</a>
  ·
  <a href="#架构">架构</a>
</p>

</div>

---

## 这是什么

积分矩阵把散落在多个 AI 平台上的「签到 / 每日任务 / 抽奖」收敛成一个动作：

> **打开应用就等于点了一次「一键签到」**：签到 → 领每日任务奖励 → 抽奖，
> 三件事一次做完，顶部一条状态带实时告诉你正在处理哪个账号、排到第几个。

打开之后想再补一次，点底部「一键签到」即可 —— 它和打开 App 做的事**完全一样**。
切后台、回到前台**不会**自动签，只刷新列表（少发请求，降低风控概率）。

它不依赖任何云端服务器，凭据只存在系统安全存储里。Android 引擎跑在 `127.0.0.1` 回环；
iOS 从 0.6.9 起改用 `pointsapi://` 自定义 scheme 在进程内直连引擎（见下方「iOS 后台不再读取失败」），
彻底不碰本机监听端口。

**核心取舍：宁可少做一个功能，也不把凭据发到云端。**

## 特性

| | |
|---|---|
| ⚡ **打开即全套** | 冷启动自动跑「签到 → 领任务奖励 → 抽奖」，与手动点「一键签到」走的同一条代码路径 |
| 📊 **状态可见** | 顶部活动条逐个账号显示进度（`百度搭子 正在签到…（2/3）`），完成后变绿、失败变红，几秒自动收起 |
| 🔒 **本地优先** | 凭据进 iOS Keychain / Android Keystore，不经任何第三方服务器；引擎不出本机（Android 回环 / iOS 进程内 scheme） |
| 🛡️ **iOS 后台不失联** | 面板走 `pointsapi://` 进程内分发，零 TCP socket，切后台再久回来也不会「读取失败」 |
| 🖼️ **图标按规范出图** | 13 尺寸 appiconset 走 `Assets.car`，平铺兜底按 `name@2x/@3x` 命名——之前主屏拿的是 60px 放大图，这才是糊的真因（0.7.2） |
| 🚀 **回前台不失灵** | 离开超过 10 秒就换一个全新 WKWebView，不问进程死活——卡住时系统根本不回话，判断本身就不该做（0.7.2） |
| 🎨 **双端同构** | UI 由 Go 侧单文件 HTML 提供，改界面只改一份，iOS 与 Android 完全一致 |
| 📱 **三种视图** | 完整卡片 / 紧凑列表一键切换；长按任意账号可拖动排序 |
| 🌗 **双态主题** | 浅色 / 深色，状态栏图标明暗同步（「跟随系统」在 WebView 里不可靠，已移除） |
| 💾 **免登录迁移** | 备份导出为真实文件（系统文件面板选位置），换机导入即恢复全部账号与登录态 |
| 🧭 **内置运行日志** | 设置页可直接看每条失败请求的时间、路径、状态码与业务码，一键复制反馈；凭据自动脱敏 |
| 🔑 **API 令牌管理** | AiPy 的调用密钥（`sk-` 开头）可在 App 内新建 / 启停 / 删除，额度、模型限制、IP 白名单都能改；误删正在用的那条会被拦下 |
| 🔔 **签到通知** | 企业微信 / 钉钉 / 飞书 / Bark / 自定义 Webhook 五渠道，启用后每轮签到推一条汇总，失败项置顶 |
| 🪶 **零框架** | 前端不用框架、不用打包器，包体为零，首屏无白屏等待 |

## 截图

<div align="center">
  <em>（此处可放置 3 张截图：卡片视图 / 紧凑视图 / 抽奖面板）</em>
</div>

## 支持平台

| 平台 | 认证方式 | 签到 | 任务 | 抽奖 | API 令牌 |
|---|---|:---:|:---:|:---:|:---:|
| WorkBuddy | OAuth token | ✅ | — | — | — |
| 灵犀（kdocs） | cookie | ✅ | — | — | — |
| 百度搭子 | cookie | ✅ | — | ✅ | — |
| AiPy | cookie | ✅ | ✅ | — | ✅ |
| Trae | JWT | ✅ | — | — | — |
| MiniMax | cookie（盐值待确认） | 🚧 | — | — | — |
| 豆包 | 页面自动化 | 🚧 | — | — | — |
| Qoder | 桌面端指纹 | ❌ 仅展示 | — | — | — |

> 「API 令牌」指平台对外发放的调用密钥（`sk-` 开头），与登录凭据是两回事。
> 目前只有 AiPy 控制台提供这套接口，其余平台没有对应能力，因此菜单里不会出现该项。

## 安装

### Android

1. 下载 [`Points.apk`](https://github.com/ElyYinLi/aipoints-mobile/releases/download/android-latest/Points.apk)
2. 允许「安装未知来源应用」
3. 安装后打开，点「添加账号」，用官方网页登录

> 覆盖升级不会丢账号：APK 签名密钥固定（`CN=Points`），可直接覆盖安装。

### iOS

自签 IPA，需要重签工具：

1. 下载 [`Points.ipa`](https://github.com/ElyYinLi/aipoints-mobile/releases/download/ios-latest/Points.ipa)
2. 用 [SideStore](https://sidestore.io/) / AltStore / Sideloadly 重签安装
3. 首次打开信任开发者证书（设置 → 通用 → VPN与设备管理）

## 本地运行

引擎可以脱离手机独立跑，用来调接口最方便：

```bash
cd core
go test ./...
go run ./cmd/pointsctl -action serve -dir /tmp/pts
# 浏览器打开 http://127.0.0.1:7879
```

## 架构

```
┌──────────────────────────────────────────────────────┐
│  iOS (SwiftUI + WKWebView)   Android (Java + WebView)│
│         └────────── 同一份 HTML 面板 ──────────┘      │
├──────────────────────────────────────────────────────┤
│  core/  Go 引擎（唯一有业务逻辑的地方）               │
│    provider/   平台适配器，统一 Provider 接口          │
│    panel/      面板服务 + 内嵌单文件 HTML + 进程内 Dispatch│
│    store/      账号存储抽象（壳层换成 Keychain/Keystore）│
│    scheduler/  当日随机错峰（FNV-1a 稳定哈希）         │
│    mobile/     gomobile bind 导出层（JSON 字符串进出） │
└──────────────────────────────────────────────────────┘
```

### iOS 后台不再「读取失败」（0.6.9，方案 A2）

长期困扰的「切后台久了回来显示读取失败」，根因不在前端：Apple 在
[TN2277](https://developer.apple.com/library/archive/qa/qa1838/_index.html) 明确说明，App 被
suspended 后监听 socket 停止 accept、资源可能被系统回收，回到前台旧 listener 也不会自动恢复。
过去只重建 WebView、改 HTTP keep-alive 都改错了链路。

0.6.9 从架构上绕开这条不可靠链路：

- **页面 + 全部 API 走 `pointsapi://panel/`**：`WKURLSchemeHandler` 拦截后调用 gomobile 导出的
  `PanelDispatch`，在进程内直接复用 panel 的同一套 handler（业务逻辑只有一份），**零 TCP socket**，
  iOS 的后台挂起机制无从切断。
- **前端零改动**：`fetch('/api/xxx')` 在 `pointsapi://panel/` 页面下自动解析为同源 scheme 请求；
  POST body 除 `httpBody` 外额外写进 `X-Points-Body` 头兜底（个别 iOS 版本会 strip 掉自定义
  scheme 的请求体）。桌面 HTTP server 忽略该头，无副作用。
- **本机 server 仅保留**给 WorkBuddy OAuth 302 回调和桌面 `pointsctl` 调试用，不再是 UI 数据链路。

图标同时更新为 **V4 错位呼应**（主界面左上角同款白色空心 spark 双星，小星描边减细保证空心清晰），
App 图标 / 面板 SVG / Android 矢量图四处同源。

### 回前台不再「点哪都没反应」——不再判断，直接换实例（0.7.2）

0.7.1 的「先探活、再重建」**也失败了**。用户实测：锁屏 4 分钟回来「卡顿恢复不了，一直卡」。

复盘出两个致命点：

1. **探活的判据不可靠**。`evaluateJavaScript("1+1")` 能回话，只说明 JS 引擎还在跑，
   **不等于输入事件能被处理**。WebView 卡住时常见的是「脚本活着、触摸全吞」，
   这时探针返回 2，代码判定「健康」，于是只刷新、不重建 —— 正好把自己锁死在卡死状态。
2. **恢复链路太长**。0.7.1 是「先 `load()` → 1.2s 没加载完 → 再换 `.id()`」，
   一次恢复跨两个 runloop、三个 `asyncAfter`。任何一环没排上（主线程一忙就排不上），
   整条链就断在半路，而 `recovering` 还停在 true，后续所有回前台事件都被
   `if recovering { return }` 挡掉 —— 于是「一直卡，恢复不了」。

0.7.2 的做法是**放弃判断**：

| 离开时长 | 动作 | 理由 |
|---|---|---|
| < 1.5s | 只调一次 `__pointsRefresh()` | 页面肯定活着，重建反而是抖动 |
| 1.5s ~ 10s | 探一次活，800ms 不回就重建 | 签到可能正在跑，不能粗暴打断 |
| **≥ 10s** | **直接换新 WKWebView** | 签到早该结束；这一档就是「卡死」的现场，不问了 |

`hardReset()` 在**一个 runloop 内**完成三件事：断开旧实例的 `navigationDelegate`
（防止它的延迟回调污染新状态）→ `webView = nil`（立刻放掉强引用）→ 换 `webGen`
（SwiftUI 建全新实例）。不做 `load()`：旧实例已经不处理消息，load 也是白发。

3 秒还没 `didFinish` 就认为这次也失败，显示**原生重试页**（「页面没有响应 / 重新加载」），
至少用户不用杀进程重开。账号与签到记录都在 Go 侧落盘 + Keychain，重试不影响数据。

顺带压掉两处长期开销：

- `isInspectable` 只在 `#if DEBUG` 开。release 下开着会让 WebKit 常驻保留调试元信息；
- `isOpaque = true`（原来是 clear）。透明会让 WebView 每帧与父视图重新合成，
  叠上 appbar 的 `backdrop-filter` 就是「滑一下要喘口气」。

还有一个隐蔽的：`StatusBarStyle.dark` 原来是 `@Published`，**赋相同值也会发
`objectWillChange`**。页面每次 `render()` 都可能 postMessage 一次 `setTheme`，
等于让根视图和 `preferredColorScheme` 反复重算。现在同值直接短路。

### 图标一直糊，是因为 iOS 拿到的是 60px 放大图（0.7.2）

前两版都在改图（圆角、alpha、描边粗细），**方向从头到尾就错了**。真正的问题在
`Info.plist` 的声明：

```
CFBundleIconFiles = ["AppIcon60", "AppIcon120", "AppIcon180"]
```

iOS 解析这个数组的规则是「**基础名 + @2x/@3x**」。对 `AppIcon60` 它只会找：

```
AppIcon60.png     (1x,  60px)  ✅ 存在
AppIcon60@2x.png  (2x, 120px)  ❌ 不存在
AppIcon60@3x.png  (3x, 180px)  ❌ 不存在
```

而 `AppIcon120`、`AppIcon180` 是**另外两个基础名**，系统匹配 `AppIcon60` 时
根本不会看它们。于是所有 @3x 屏要 180px 图标，系统只找到 60px 那张，
**放大 3 倍**铺上去 —— 源图画得再锐利，主屏上都是糊的。

同时 `CFBundleIconName` 从来没写过，所以编译好的 `Assets.car`（13 尺寸全套）
根本没被用上。

修法两条路都铺对：

1. `CFBundleIconName = AppIcon` → 系统读 `Assets.car`，那里是 13 尺寸、RGB 无 alpha、
   满幅不预圆角的全套图（`tools/gen_icon.py` 2048 超采样后 LANCZOS 缩）；
2. 平铺文件按 `AppIcon60@2x.png` / `AppIcon60@3x.png` / `AppIcon76@2x.png` /
   `AppIcon83.5@2x.png` 正确命名，兜住「自签重签工具破坏 Assets.car」的情况；
3. `CFBundleIconFiles` 只列**基础名**（`AppIcon60` / `AppIcon76` / `AppIcon1024`），
   让系统自己拼后缀。

### ~~锁屏 / 后台回来「点哪都没反应」的根治（0.7.1）~~（实测无效，0.7.2 已替换）

> **保留此节只作反面记录。** 「探活 + 两级重建」在真机上仍然卡死，
> 原因见上一节。现在的实现是 0.7.2 的「直接换实例」。

0.7.0 判断错了方向。当时以为是 WebKit 拒绝「前台活动」，于是把调用延后 350ms。
**实测无效**：锁屏 4 分钟回来，整个界面完全没反应。

真正的原因是 WebContent 进程被**冻结**（不是被 kill）之后根本没有恢复：

- 页面收不到任何事件 → 点哪都没反应；
- `evaluateJavaScript` 的 completionHandler 永远不执行；
- `webViewWebContentProcessDidTerminate` **也不会触发**（进程没死）。

[WebKit Bug 321511](https://bugs.webkit.org/show_bug.cgi?id=321511) 的现场记录：进程
`suspended and idle`、CPU 0.004s、主线程停在 `mach_msg` 正常空闲、
`Time Since Fork` 23 小时、既没被 kill 也没 reload。客户端**无法唤醒**这种进程。

所以 0.7.1 改成「先探活，死了就重建」：

| 离开时长 | 动作 |
|---|---|
| < 1.5s | 快速切一下，进程肯定活着，直接刷新 |
| ≥ 1.5s | 发 `1+1` 探针，**700ms** 内没回来 → 判定冻结 |

判定冻结后两级重建：

1. **先 `load()` 面板入口**（本地 HTML，正常几十毫秒）；1.2s 还没加载完说明连 load 都发不出去；
2. **销毁重建整个 WKWebView**（改 `.id()` 强制 SwiftUI 换实例）——这是进程池整体卡死时
   唯一能自救的手段。

重建期间盖一层同色遮罩，看起来是「刷新了一下」而不是白屏。

另外补了一个**必须有的崩溃防护**：`WKURLSchemeTask` 一旦被 `stop`，再调
`didReceive` / `didFinish` 会抛异常直接崩溃。而重建必然 cancel 上一轮所有
in-flight 请求，Go 侧跑完照样会回调——旧代码注释写了「忽略已停止的 task」，
但**没有真的检查**。现在用活跃集合守住。

重建后走 `bootCheck()` → `/api/daily` 判定，今天跑过就只刷新，**不会重复签到**；
账号在 Go 侧落盘 + Keychain，与 WebView 无关，不会丢。

### ~~图标描边减细（0.7.1）~~（不是糊的原因，0.7.2 已找到真因）

> **保留此节只作记录。** 描边从 48/28 减到 30/17 本身不算错（白色占比 8.03% → 5.23%，
> 更接近参考实现的 3.96%），但它**不是**「图标不清晰」的原因——iOS 当时根本没在
> 使用这张图，见上一节。

0.7.0 保留 48/28 描边，只做了规范修正。这一版按实测结论减细到 **30/17**：

- 同尺寸（137px）逐像素比对 `aipy-checkin`：白色像素占比 **8.03% → 5.23%**（参考实现 3.96%）
- 48 太粗，主星四角之间的凹口被填平，星形退化成一团十字
- 副星按同比例（17/30）减细，保持空心
- 左上角 logo 同步：主 1.10 / 副 0.62（保留相对比例，不按 42.67 严格缩——那样副星会细到消失）

### 长时间后台回来不再整体卡顿（0.7.0）

0.6.9 解决了「读取失败」，但留下另一个问题：**后台待久了回前台，整个界面卡顿，点一下要反应半天**。

根因在 WebKit 的进程挂起机制。iOS 在 App 即将挂起时调用 `processWillBeSuspendedImmediately()`
→ `setProcessesShouldSuspend(true)` → `ProcessThrottler::setAllowsActivities(false)`，
此后页面**不接受任何前台活动**，绘制区还会 `hideContentUntilPendingUpdate()` 清空图层树。
解除挂起依赖 RunningBoard 的**异步**状态回调，回前台瞬间还没生效——而原来代码一
`didBecomeActive` 就调 `evaluateJavaScript`，它底层正是 `runJavaScriptInFrameInScriptWorld`，
在这个窗口里会被直接拒绝，后续刷新与交互全部排队。WebKit
[PR #66838](https://github.com/WebKit/WebKit/pull/66838) 的日志里就记着这条拒绝。
另据 [Bug 321511](https://bugs.webkit.org/show_bug.cgi?id=321511)，进程被**挂起**（而非 kill）时
`webViewWebContentProcessDidTerminate` 不会触发，页面会一直僵着。

0.7.0 当时做了三层，其中**第一条是错的**（0.7.1 已推翻，见上一节）：

- ~~回前台延后 350ms 再调 JS~~ —— 以为能避开 RunningBoard 的窗口，实测无效，0.7.1 删除。
- **补上 `webViewWebContentProcessDidTerminate`**：进程真被系统回收时自动重新加载面板，
  不再出现「点什么都不响应」。带 **60s 冷却**，避免崩溃循环里反复闪白。
  重载后走 `bootCheck()` → `/api/daily` 判定，今天跑过就只刷新，**不会重复签到**。
- **降 GPU 常驻开销**：`.appbar` / `.bar` 的 `backdrop-filter` 从 `blur(18px)` 压到 `blur(10px)`
  并加 `@supports` 降级（这两个面一个 sticky 一个 fixed，长期停在屏幕上，每帧都要重算模糊）；
  删掉 `.card` 常驻的 `will-change:transform`（每张卡一个合成层，账号多了反而拖慢合成）。

### 图标产物规范化（0.7.0）

这一版只动生产流程，**图形和描边都没改**（描边在 0.7.1 才减细）：

- **满幅方形、不预烘焙圆角**：iOS 自己按 ≈22.37% 裁圆角；预烘焙后圆角外侧的透明像素
  在部分渲染路径（第三方商店 / 重签安装）会被填成纯黑，边缘发脏。
- **RGB 无 alpha 通道**：App Store 强制要求，透明区会被渲染成纯黑。
- **完整 13 尺寸 `AppIcon.appiconset` + 18 条 `Contents.json`**：以前只有 4 张平铺 PNG 靠
  `sips` 现缩，主屏 / 设置页 / App Store 三种尺寸各糊一遍。
- 由 `tools/gen_icon.py` 统一产出，`--check` 可校验合规性；`ios/build-ipa.sh` 不再现场缩放。
- **左上角 logo 与 App 图标同源**：以前是两枚**实心**星，和 App 图标的空心描边双星是完全独立的
  两套图形，改 App 图标时 logo 永远不跟着变；现在按 1024 画布坐标等比映射到 24 网格。

### 目录

```
core/                 Go 引擎
  provider/           平台适配器：workbuddy / aipy / dumate / lingxi / trae
                      + 能力扩展：task.go / lottery.go / token.go / errkind.go
  panel/              面板 HTTP 服务 + 内嵌前端（index.go / icons.go）
                      + 规则加固：task_memo.go（任务去重）/ notify.go（Webhook）
  httpx/              零依赖 HTTP 层
  scheduler/          当日随机错峰排期
  store/              账号存储（含拖拽排序的 Order 字段）
  mobile/             gomobile bind 导出层
  cmd/pointsctl/      命令行验证工具
android/              薄 Java 壳：WebView + 登录容器 + 前台服务
ios/                  SwiftUI + WKWebView + Keychain
spec/api-map.json     接口地图（机器可读）
docs/                 逆向分析、设计决策、沙箱踩坑记录
.github/workflows/    CI：Linux 出 APK，macOS 出 IPA
```

### 能力接口

平台能力用「可选接口 + 类型断言」表达，新增能力不需要改所有适配器：

```go
type TaskClaimer interface {          // 每日任务领取
    Tasks(ctx, cred) ([]Task, error)
    ClaimTask(ctx, cred, taskType) (ClaimResult, error)
}

type LotteryProvider interface {      // 签到后抽奖
    DrawStatus(ctx, cred) (DrawStatus, error)
    Draw(ctx, cred) (DrawResult, error)
}

type TokenManager interface {         // API 令牌（Key）管理
    ListTokens(ctx, cred) ([]APIToken, error)
    CreateToken(ctx, cred, in TokenInput) (APIToken, error)
    UpdateToken(ctx, cred, in TokenInput) error
    DeleteToken(ctx, cred, id int64) error
    SetTokenStatus(ctx, cred, id int64, status int) error
}
```

`Registry.All()` 自动探测并写入 `Meta.Tasks` / `Meta.Lottery` / `Meta.Tokens`，
前端据此决定「···」菜单里出现哪几项。

错误分类同样走统一枚举（`core/provider/errkind.go`）：`auth` / `already` / `captcha` /
`rate_limit` / `blocked` / `network` / `server` / `unknown`。分类决定了该不该给「重试」：
只有 `network`、`server`、`rate_limit` 是可重试的，凭证失效和风控冻结重试也没用。

## 开发

```bash
# 全量测试
cd core && go test ./...

# 构建（需要 ANDROID_HOME）
bash android/build-apk.sh

# 构建 iOS（只能在 macOS）
bash ios/build-ipa.sh
```

CI：`core-ci` 每次 push 必跑（gofmt / vet / test / 密钥扫描 / **面板内联 JS 语法检查**）。
打包工作流 `build-android` / `build-ios` 改为**仅手动触发**：它们在公开仓库虽免费，
但 paths 过滤会让每次纯代码提交都触发一轮打包，白烧构建时长。

### 在受限沙箱里同步代码

如果 `git push` 反复超时（沙箱网络会拦 git 协议端点，带 token 也一样），别修它，改走 REST API：

```bash
export GH_TOKEN=github_pat_xxx
python3 tools/gh_sync.py --repo OWNER/NAME --dry-run   # 看差异
python3 tools/gh_sync.py --repo OWNER/NAME             # 真推
```

原理与全部实测坑见 [`docs/SANDBOX-GITHUB.md`](docs/SANDBOX-GITHUB.md)。发版前务必跑 `python3 tools/check_secrets.py .`（CI 已强制执行）。

## 给接手的人

如果是 AI 或新同学接手这个项目，先读 **[docs/AI-交接文档.md](docs/AI-交接文档.md)**。

那份文档包含：仓库地图、三个抽象接口、**27 个已踩过的坑**（含每个坑的根因与修法）、
各平台接口清单、加新平台的完整清单、推代码方式、CI 与 Release 机制的用法、界面代码导航。

## 排查问题

出错时不用装任何调试工具：**设置 → 运行日志**里能看到最近 400 条记录。

| 列 | 含义 |
|---|---|
| 时间 | 到秒，便于对照「我几点点的按钮」 |
| 方法 + 路径 | 哪个平台接口，`POST /api/dumate/activity/growth-plan/draw` 这种形式 |
| 状态 / 业务码 | `HTTP 401` 是登录失效；`code 410110` 是网关幂等 ID 被拒 |
| 消息 | 服务端返回的原因，或网络层错误（超时、连接被拒） |

三条约定：

1. **只记异常**。成功的查询不写日志，否则一次下拉刷新就能刷掉几十条，把真问题埋掉。
2. **凭据一律脱敏**。`cookie` / `authorization` / `BDUSS` / `stoken` / 32 位以上连续 hex 全部替换成 `***`，日志可以放心贴出来。
3. **主动取消不记**。连续下拉刷新会取消上一轮请求，这属于正常交互，不算错误。

其余常见情况：

- **显示「今日已签」没有 `+N`**：平台接口没回具体数值。此时只写状态，绝不用 `+?` 这种占位糊弄 —— 宁可不显示，也不显示一个看起来像出错的东西。
- **百度搭子一打开就是「已签」**：它的每日积分在你当天第一次访问网页时就由平台自动发放了，我们看到的是真实状态，不是判断错误。
- **切后台回来不签到**：这是刻意设计。只有打开 App 那一次会自动签，之后请手动点「一键签到」。
- **后台久了回来读取失败**：系统回收了 WebView 的渲染进程，页面已经死了。App 会先探活，探不到就整页重建（先 `about:blank` 再回面板），不需要手动杀进程。
- **提示「未能确认到账」**：签到请求发出去了，但紧接着的回查没看到 `今天已签`。这既可能是真没到账，也可能是上游有延迟——所以下拉刷新核对一下，不要立刻重复点签到。
- **提示「该账号正在签到中，已跳过」**：同一个账号上一轮还没跑完就又被触发了一次。这是互斥锁在起作用，防止连点把同一份请求打两遍上游。
- **通知没收到**：先在「设置 → 签到通知」点「发测试」。测试走的是和正式推送**完全相同**的代码路径，测试不通就是地址/渠道选错了。4xx（地址失效）会自动关闭通知并在设置页挂横幅，5xx 只累计失败次数。
- **Webhook 为什么必须由 Go 发**：JS `fetch` 会带上 `Origin: http://127.0.0.1:...`，企业微信和钉钉的网关会直接拒收；而且 App 切后台时 JS 请求会被系统掐断。所以前端只负责把这一轮结果整理好交给 `/api/notify`。

## 设计约束

每一条都是踩过坑之后定下来的：

| 约束 | 原因 |
|---|---|
| 凭据只进 Keychain / Keystore，不进文件 | 桌面版明文落盘是它最大的安全缺陷，这里不重复 |
| 登录一律走官方网页（系统 WebView） | 不碰用户密码；平台改 OAuth 流程也不影响我们 |
| 同平台多账号用独立 WebView 数据仓 | 否则 cookie 串号，签到会签到别人账上 |
| 签到串行 + 平台间 1.2s 间隔 + 当日随机错峰 | 批量并发是风控触发的最主要原因 |
| 只有冷启动自动签，回前台只刷新 | 一个下午切十次后台就签十轮，风控必中 |
| 拿不到数值就不显示 `+N` | 显示 `+?` 比不显示更像软件坏了 |
| 抽奖 `request_id` 每次全新生成 | 上游按幂等键判重，复用会被拒 |
| gomobile 产出**静态库**而非 dylib | dylib 会被放进 `Frameworks/`，多数自签工具报 `Invalid mach-o file!` |
| UI 走本地 HTML 而不是原生声明式 | 一套代码双端复用，不引入 SwiftUI/Compose 两套成本 |
| 混合单位不显示总数 | 100 灵石 + 12.5 积分加起来没有任何含义 |
| 签名密钥随仓库走 | 换密钥 = 用户必须卸载重装 = 账号全丢 |
| 任务奖励按 `日期\|平台\|账号\|任务` 四维去重 | 只在单维度去重会让两个平台同名任务互相顶掉；只有真拿到奖励（`>0`）才记账 |
| 签到成功后回查一次再报成功 | 上游 200 不等于到账；回查本身失败时不判失败，避免把成功误报成失败 |
| 推送失败绝不影响签到结果 | 推送是附加价值；`/api/notify` 一律返回 `ok=true`，只带 `sent` 与 `error` |
| 脱敏在发送前做，不在展示时做 | Webhook 是把内容推出设备，cookie/key 一旦进去等于发给第三方留档 |

## 致谢

接口事实来自对「AI 积分助手 v2.2.0」的静态分析（未加壳未混淆），本仓库不复用其任何代码，仅复用接口知识。详见 [`docs/逆向分析报告.md`](docs/逆向分析报告.md)。

## 许可证

[MIT](LICENSE) —— 仅供个人学习与自用。
