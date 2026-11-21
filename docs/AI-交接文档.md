# AI 交接文档

> 给下一个接手这个项目的 AI（或人）看的。目标：读完这一份就能独立继续开发，不用重新逆向、不用再踩已踩过的坑。
>
> 最后更新：2026-11-21 · 对应版本 **0.7.2 (versionCode 27)**

---

## 0. 三十秒速览

**项目是什么**：一个把多家 AI 平台「每日签到 / 领积分 / 抽奖」合到一起的手机 App，名字叫**积分矩阵**。

**技术形态**：Go 写业务引擎，`gomobile bind` 编成 `.aar/.xcframework`，iOS(Swift) / Android(Java) 只做一层原生壳 + WebView；界面是 Go 里内嵌的**单文件 HTML**。加载方式两端不同：**Android** 跑本机 `127.0.0.1:7879` 的 HTTP 服务；**iOS（0.6.9 起）** 走 `pointsapi://panel/` 自定义 scheme，由 `WKURLSchemeHandler` 在进程内直连 Go，**零 TCP socket**。

**为什么这么设计**：界面只写一份两端通用；业务逻辑只写一份 Go；凭据不出本机；平台接口改版时只需改一个 provider 文件。

**当前行为（0.7.2，改动前务必确认没有破坏）**：

- **打开 App 走「今日判定」**：引擎落盘 `daily_memo.json`，今天跑过就只刷新 + 列出哪几个没签，不再发签到请求；没跑过才走完整一键签到（签到 → 领任务奖励 → 抽奖，与点底部按钮同一条 `checkinAll` 路径）。**一键签到按钮每次都真跑一轮，不受「今天跑过」限制**——这条是用户红线，别动。
- **切后台/回前台**只刷新列表，不发任何写请求（用户要求，避免重复请求触发风控）。
  但「回前台换 WebView 实例」是例外，而且是必须的：0.6.4 曾把整页重建/探活全删掉，
  0.7.0~0.7.2 证明那是错的——WebView 长时间挂起后真的会僵死。现在的策略见 3.37：
  离开 <1.5s 只刷新 / 1.5~10s 探活（不打断签到）/ **≥10s 直接换新 WKWebView**。
  **换实例不等于重跑签到**：新页面走 `bootCheck()` → `/api/daily` 判定，今天跑过就只刷新。
- **iOS「读取失败」根治（0.6.9 方案 A2）**：真因不是前端、也不是 keep-alive（那改的是引擎→外部平台链路），而是 Apple TN2277 所述——App suspended 后本机 listening socket 停止 accept、资源可被回收，回前台旧 listener 不再恢复。0.6.9 让 iOS 主文档 + 全部 API 走 `pointsapi://panel/`，`WKURLSchemeHandler` → `MobilePanelDispatch` → `panel.Server.Dispatch`，进程内复用同一套 handler，不经任何 socket。本机 server 只留给 WorkBuddy OAuth 302 与桌面 `pointsctl`。改这条链路前务必读 `ios/App/Sources/PanelSchemeHandler.swift` 头注释与本文第 3 节。
- 签到进度全程显示在顶部活动条（`#act`），逐账号 `正在签到…（2/3）`
- **没有「···」入口**（0.6.4 全删）：任务 / 抽奖直接摆卡片与紧凑行；令牌 / 停用 / 删除放在 设置 → 账号 的行内按钮；长按账号名改备注
- **通知（0.6.7 起「启用即推」）**：「什么时候推」四个触发开关 + 「重发上次」全删，只要通知启用，每轮签到都推一条 Webhook（五渠道），失败项置顶；`on_start/on_manual/always` 存量值在 save/send 时强制归一化成 true，不参与判断

**界面结构**：主界面顶栏 = logo(spark 双星，0.6.9 起为 V4 错位呼应：大星居中 + 小星右下；0.7.1 起描边 30/17，与 App 图标同源) + 标题 + 刷新/主题/设置；设置弹层四个页签 通知/账号/数据/日志（无标题栏、无关闭按钮，点遮罩关闭）；版本号只在「签到逻辑」页底部；数据页导出行 = 生成/复制/导出 三按钮一行。

**0.6.0 新增的规则（都在 `core/panel/` 里，改之前先读对应文件头注释）**：

| 规则 | 文件 | 一句话说明 |
|---|---|---|
| 任务去重 | `task_memo.go` | 键为 `日期\|平台\|账号\|任务类型`，只有 `Gain>0` 才记账 |
| busy 互斥 | `panel.go` | 键为 `平台\|账号`，防连点把同一份请求打两遍 |
| 签到二次核对 | `panel.go` | 成功后再 `Query` 一次，未确认则报「未能确认到账」 |
| 错误分类 | `provider/errkind.go` | 8 类枚举，只有 network/server/rate_limit 可重试 |
| 通知 | `notify.go` | 五渠道报文 + 发送前脱敏 + 4xx 自动停用 |
| 令牌 | `api_tokens.go` | 删除保护：key 正好是当前登录凭证时拒绝 |

**仓库**：`github.com/ElyYinLi/aipoints-mobile`（**公开**）· 分支 `main` · MIT

**接手三件事**（按顺序）：

1. 读 `core/provider/` 下每个 provider 的头部注释——里面写着每个平台的接口清单和字段名来源
2. 读本文第 3 节的「坑」——90% 的返工都出在那些地方
3. 本地跑 `cd core && go test ./...`，应该是全绿；再跑 `go run ./cmd/pointsctl -action serve` 打开 `http://127.0.0.1:7879` 看界面

---

## 1. 仓库地图

```
.
├── core/                      ← Go 模块（go.mod 在这里，不在根目录！）
│   ├── httpx/                 HTTP 工具层 + 运行日志
│   │   ├── client.go          DoJSON、Response.JSON、Num/Str/Bool/Pick 取值助手
│   │   └── log.go             环形日志缓冲 + 脱敏（★ 0.5.1 新增）
│   ├── provider/              ★ 业务核心：每个平台一个文件
│   │   ├── provider.go        Provider / LotteryProvider / TaskClaimer 接口与数据结构
│   │   ├── registry.go        注册表，按 ID 查 provider
│   │   ├── workbuddy.go       WorkBuddy（token 型 + OAuth）
│   │   ├── lingxi.go          金山灵犀（cookie 型）
│   │   ├── dumate.go          百度搭子（cookie 型 + 抽奖）
│   │   ├── trae.go            Trae（token 型）
│   │   ├── aipy.go            AiPy（cookie 型 + 任务领取）
│   │   └── helpers.go         包内共用小工具
│   ├── store/                 账号持久化（accounts.json）+ 排序
│   ├── scheduler/             随机签到排期（稳定伪随机 + 最小间隔）
│   ├── panel/                 ★ 内嵌面板：HTTP 路由 + 单文件 HTML 界面
│   │   ├── panel.go           路由注册、checkin/query 主流程
│   │   ├── index.go           ★★ 约 2100 行，整个 UI（HTML+CSS+JS）都在这一个文件里
│   │   ├── icons.go           SVG 图标表，HTML 里写 {{ICON:name}} 占位符
│   │   ├── api_*.go           按功能拆的 handler
│   │   └── api_logs.go        运行日志接口（★ 0.5.1 新增）
│   ├── mobile/                gomobile 导出层（只管 JSON 字符串进出）
│   └── cmd/pointsctl/         命令行工具：serve（起面板）/ query / checkin
├── ios/                       Swift 壳。⚠️ 不是 Xcode 工程，是 build-ios.sh 手写编的
│   └── App/Sources/
│       ├── PointsApp.swift    WKWebView 装配 + scheme handler + 导航策略（★ 0.7.2：离开≥10s 直接换实例，不再探活）
│       ├── PanelSchemeHandler.swift  ★ 0.6.9 A2：pointsapi:// 拦截 → MobilePanelDispatch
│       ├── Engine.swift       引擎门面（panelURL 指向 scheme baseURL）+ Keychain
│       └── LoginCoordinator.swift / LoginView.swift  OAuth/points:// 登录容器
├── android/                   Java 壳 + 零 Gradle 构建脚本 build-apk.sh
├── .github/workflows/         core-ci / build-android / build-ios
├── assets/
│   ├── app-icon.svg           图标源（满幅方形，无 rx 圆角）
│   └── AppIcon.appiconset/    ★ 0.7.0：完整 13 尺寸 + 18 条 Contents.json（RGB 无 alpha）
├── tools/
│   ├── gen_icon.py            ★ 0.7.0 图标生成器，--check 校验合规（无 alpha / 满幅 / 尺寸齐）
│   ├── gh_sync.py             ★ 沙箱里推代码用它（见 5.1）
│   ├── push-via-api.sh        上面那个的薄封装，等价，习惯用哪个都行
│   └── check_secrets.py       提交前扫密钥
└── docs/                      逆向笔记、平台接口备忘、设计约束
```

---

## 2. 核心抽象：三个接口

所有平台能力都收敛成三个**可选**接口，`provider.Meta` 里的布尔字段决定 UI 是否显示入口。加新平台**不要**改这三处以外的任何地方。

### 2.1 Provider（必需）

```go
type Provider interface {
    Meta() Meta                                                    // ID/名称/鉴权方式/是否支持移动端
    Query(ctx, Credential) (Snapshot, error)                        // 查余额 + 今日是否已签
    Checkin(ctx, Credential) (Result, error)                        // 签到
}
```

### 2.2 TaskClaimer（可选：有「领任务奖励」的平台）

```go
type TaskClaimer interface {
    Tasks(ctx, Credential) ([]Task, error)
    ClaimTask(ctx, Credential, taskType string) (Result, error)
    ClaimAll(ctx, Credential) ([]Result, error)
}
```

### 2.3 LotteryProvider（可选：有「抽奖」的平台）

```go
type LotteryProvider interface {
    DrawStatus(ctx, Credential) (DrawStatus, error)   // 剩余次数
    Draw(ctx, Credential) (DrawResult, error)         // 抽一次
}
```

启用方式：在 provider 文件末尾写 `var _ LotteryProvider = (*Xxx)(nil)`，`registry.All()` 会自动用类型断言推断 `Meta.Lottery = true`，UI 自动出现「抽奖」入口。**不需要**在别处登记。

### 2.3b TokenManager（可选：有「API 令牌」管理的平台，0.6.0）

```go
type TokenManager interface {
    Provider
    ListTokens(ctx, Credential) ([]APIToken, error)
    CreateToken(ctx, Credential, TokenInput) (APIToken, error)
    UpdateToken(ctx, Credential, TokenInput) error
    DeleteToken(ctx, Credential, id int64) error
    SetTokenStatus(ctx, Credential, id int64, status int) error
}
```

**不要把 `APIToken.Key` 和 `Credential.AccessToken` 搞混**：前者是平台对外发放的调用密钥（`sk-` 开头，用来调 API），后者是这个账号登录用的凭证。两者恰好可能是同一个字符串——这正是删除保护存在的理由（`api_tokens.go` 里比对 `in.Key == cred.AccessToken`）。

AiPy 的接口形状有三处「不正常」，必须照抄，别「优化」：

- 列表两条路径回退：`/api/token/page?p=1&size=100` 失败后改走 `/api/token/?p=0&size=100`
- 删除路径**带尾斜杠**：`DELETE /api/token/{id}/`（少了就是 404）
- 启停不是 PUT，是 `PUT /api/token/?status_only=true`，body 只传 `{id, status}`
- `expired_time=0` 上游理解为「立刻过期」，新建时必须补成 `-1`（`tokenBody` 里做）

### 2.4 关键数据结构

```go
type Snapshot struct {
    Provider, AccountID string
    Label               string   // 面板回填，provider 不产生
    OK                  bool
    Balance             float64
    BalanceText         string
    Unit                string   // 空 = 「积分」
    CheckedIn           bool
    TodayGain           float64  // ★ 今日签到可得/已得，为签到回执服务
    Streak              int
    Detail, Error       string
    At                  time.Time
}

type Result struct {
    Provider, AccountID string
    OK, Already         bool
    Gain                float64
    Unit                string   // ★ 必填！前端靠它拼「+N 单位」
    Message, Error      string
}
```

---

## 3. 踩过的坑（★ 最重要的一节）

这一节记录的都是**实际发生过、排查了很久**的问题。改代码前先看这里，能省一整天。

### 3.1 百度搭子抽奖报 `410110 抽奖请求ID无效`

- **现象**：抽奖次数还有 1 次，点「立即抽奖」返回 `{"code":410110,"msg":"抽奖请求ID无效"}`
- **根因**：`request_id` 用 `randHex(16)` 生成，是 **32 位纯 hex**；百度 BCE 网关按 **UUID 形状**（`8-4-4-4-12`，36 字符带连字符）校验，长度和分段都不对，直接判非法
- **修法**：改用 `randUUID()`（`dumate.go`），标准 UUID v4（version 位 4、variant 位 8/9/a/b）
- **注意**：`x-bce-request-id` 头**不校验**格式，但 body 里的 `request_id` **校验**。两者分开生成、都保持 UUID 形状
- **回归测试**：`TestRandUUIDShape`、`TestDumateDrawRequestIDIsUUID`（还断言两个 ID 不能相同，复用会被判重复请求）

### 3.2 cookie 型平台的写接口必须带 `csrftoken`

- 值从 cookie 里的 `bce-user-info` 取，且**要去掉首尾引号**
- 另需 `X-Dumate-Client-Type: web`，缺了会被当非浏览器来源，积分/状态接口**静默返回空**（不是报错，更难查）
- `quota_overview` 必须带 `?clientType=desktop&timezone=Asia%2FShanghai`，否则返回空

### 3.3 签到了但显示未签到

- 已签标志字段是 **`hasIssued`**，不是猜的 `signed_today` / `claimed` / `is_signed`——那些上游根本不存在

### 3.4 签到成功但没有「+N 积分」

- 领积分接口常常**只回 `{"code":0}`**，不回数值
- 修法两级兜底：① 状态接口里的今日积分字段 → ② **签到前后总积分差值**（`gainOf()`）
- 原则：**两者都拿不到就不显示 +N**，宁可不显示也不显示错的数

### 3.5 早上就显示「今日已签到」

- **不是 bug**。搭子的每日登录积分在**当天 0 点后首次打开网页就已自动发放**，我们只是补一次 POST 确认
- UI 上已补一句说明（`alreadyWhy()`），避免用户以为是判断错误

### 3.6 前端：`[hidden]` 打不过 `display:flex`

- 元素自身 `display` 优先于 `[hidden]` 的 `display:none`
- 修法：用类控制 `.total.off{display:none}`，不要用 `hidden` 属性

### 3.7 前端：`background-clip:text` 把破折号裁成色条

- `—` 配合 `background-clip:text` + `-webkit-text-fill-color:transparent` 会变成一根粗彩色横条

### 3.8 前端：闭包作用域

- `refresh()` 曾定义在 `openSheet` 回调**外面**，取不到回调内定义的 `setRemain`，抛 `ReferenceError` 被 `.catch` 吞掉，表现为「面板一直显示 —」
- **规则**：sheet 内部的辅助函数一律写在回调**内部**

### 3.9 前端：变量遮蔽

- `var gainTxt = ...` 会提升到函数作用域，遮蔽同名工具函数 `gainTxt()`，同函数里再调用就是 `TypeError`
- 已改名 `gText`，并在注释里写明原因

### 3.10 CSS 变量必须存在

- `var(--mono)` / `var(--hair)` 曾未定义导致样式失效。改 CSS 时先确认令牌在 `:root` 里

### 3.11 Android：`Theme.DeviceDefault.DayNight.NoActionBar` 不存在

- SDK 平台包里没有这个条目，改用 `res/values/styles.xml` 自定义 `AppTheme`

### 3.12 Android：XML 注释不能写进属性列表

- `<application>` 标签的属性中间插注释 → aapt2 报 `not well-formed (invalid token)`

### 3.13 沙箱里 `pkill -f xxx` 会杀掉自己的 shell

- 报 `Signal: SIGTERM`
- 修法：`pgrep -f "精确模式" | while read p; do kill -9 $p; done`，或启动时用 `setsid` 并记录 PID

### 3.14 Android WebView 的 `prefers-color-scheme` 不可靠

- **现象**：用户选「跟随系统」，系统明明是浅色，App 却是深色（或反之）
- **根因**：Android WebView 对 `prefers-color-scheme` 媒体查询的实现不一致，部分机型恒为 light / 恒为 dark
- **修法**：`applyTheme()` 在 system 模式下用 JS 的 `matchMedia` 主动算出明暗，**显式**写 `data-theme="dark"/"light"`，不依赖 CSS 媒体查询。CSS 里的 `@media` 只作为 JS 未执行时的兜底（选择器改为 `:root:not([data-theme])`）
- **配套**：初始化时监听 `matchMedia('(prefers-color-scheme: dark)')` 的 `change` 事件，用户切系统主题时立刻跟上；监听回调里**不能**写 localStorage

### 3.15 「今日已签 +N 积分」拿不到 N

- **现象**：状态行只显示「今日已签」，没有数字；或 WorkBuddy 显示「+100」但缺「积分」
- **三个原因叠在一起**：
  1. 各家 provider 自己拼字符串，口径不一致（有的带单位有的不带）
  2. 平台接口不一定回今日积分字段
  3. 早上打开网页时积分已自动发放，我们连 POST 都没发过，拿不到增量
- **修法**（三层）：
  - 文案**只在** 前端 `detailText()` 生成，provider 只给 `today_gain` 数值 + `unit`
  - 签到成功时用「前后总额差值」算出实得，`panel/gain_memo.go` 记到本地
  - 查询时若平台没给、memo 有值就补上，并**顺带把状态纠正为已签**（WorkBuddy 签完了状态接口仍回 false）
- **memo 细节**：带日期，跨天自动失效；`gain<=0` 视为清空；落盘到 `{DataDir}/gain_memo.json`

### 3.16 合计区「单位不同所以不合计」这段说明

- 用户明确反馈「没什么用又浪费空间」，**已整块删除**（连同 `.total-note` 节点与样式）
- 现在单位不一致时：`hero-head`（「全部账号合计 + 数值 + 单位」）整块 `display:none`，统计条保留
- 注意：判断单位是否一致前必须**归一化**（`unit || '积分'`），否则 `undefined` 会被当成新单位，把本来一致的合计误判成不可合计

### 3.17 Go 版本

- `t.Context()` 需要 Go 1.24+；本项目测试里用 `context.Background()` 更稳

### 3.18 「今日已签」后面没数字（0.5.3 根治，投诉过三次）

这是用户投诉次数最多的一条，前两次都没修对，原因是**两处同时出错**：

1. **provider 层**：`aipy.go` 把「+N 灵石」拼进了 `snap.Detail` 字符串，却**没填 `snap.TodayGain`**。面板只认数值字段 → 永远走「已签」分支，`+N` 被吞掉。
   - 修法：provider **只给数值**（`snap.TodayGain = todayReward / aipyQuotaPerStone`），`Detail` 只写状态。
2. **前端层**：`detailText()` 原来是 `if (today_gain > 0) 才拼`。数值缺失就什么都不拼，静默降级成「今日已签」。
   - 修法：已签就**无条件**拼 `+N 单位`；确实拿不到数时给 `+?` 占位，绝不再吞掉整段。

> 规则沉淀：文案只在 `detailText()` 一处生成；provider 只提供 `today_gain` + `unit`。
> `panel/index_test.go` 里有字符串断言守着这两条，谁改回去测试立刻红。

### 3.19 主题「跟随系统」不跟随（0.5.3 根治）

Android WebView 上 `@media (prefers-color-scheme: dark)` 和 `matchMedia().matches` 都会失灵（ROM/内核差异），只靠 CSS 媒体查询必然翻车。现在的三层保障：

1. **壳层真值**：`MainActivity` 用 `Configuration.uiMode` 判断系统明暗（这是系统权威值），通过 `window.__pointsOnSystemTheme(dark)` 推给页面；`onConfigurationChanged` 里系统一切换就推。
   - 前提：`AndroidManifest.xml` 的 `android:configChanges` 必须含 `uiMode`（已有）。
2. **JS 主动落属性**：`systemDark()` 先读壳层真值，再退 `matchMedia`；`applyTheme()` 显式写 `data-theme="dark"/"light"`，不让 CSS 媒体查询决定结果。
3. **每秒轮询兜底**：`setInterval` 比对真值变化，变了才重绘。代价极低，换来「切了真的跟得上」。

> 坑中坑：`localStorage.setItem('points_theme', ...)` **不会**同步刷新 `data-theme`。
> 手动改存储后必须自己调一次 `applyTheme()`，否则「切了没反应」。
> 已加 `storage` 事件监听兜住外部改写。

### 3.20 验证主题改动的正确姿势

Playwright 里模拟「WebView 媒体查询坏掉」：

```python
INIT = """const o=window.matchMedia;window.matchMedia=function(q){
  if(q.indexOf('prefers-color-scheme')>=0)return{matches:false,media:q,
    addEventListener(){},removeEventListener(){},addListener(){},removeListener(){}};
  return o.call(window,q)};"""
```

然后只调 `window.__pointsOnSystemTheme(true/false)`，看 `data-theme` 是否跟着变。
只测「正常浏览器切 color_scheme」是测不出这个 bug 的——真实机型就是坏的。

> **0.5.4 更新**：这套「跟随系统」已经**整个删除**。改了三版仍然不可靠，
> 用户明确要求「就一个浅色一个深色」。现在 `THEMES = ['light','dark']`，
> 默认深色，历史值 `system` 在读的时候会被改写成 `dark`。
> 上面 3.19 的内容仅作历史记录，**不要再照着它加回系统跟随**。

### 3.21 「今日已签 +？」——拿不到数值时的显示

0.5.3 为了「一定显示数字」用了 `+?` 占位，用户评价是「很搞笑」。
现在的规则（`TestIndexDetailTextGainRule` 守着）：

- 有 `today_gain` → `今日已签 +N 单位`
- 没有 → `今日已签`（干净收尾）

教训：**宁可少显示，也不要用占位符暴露「我拿不到数据」**。
用户看到 `?` 只会觉得软件坏了，而不是觉得平台没给。

### 3.22 签到过程必须可见（0.5.4）

原来的实现是前端一次 `POST /api/checkin`，后端串行签全部账号。
问题是**中间过程完全没有反馈**：用户打开 App 看到「未签」，几秒后变「已签」，
不知道是自动签的、还是自己之前点的、还是根本没签。用户的质疑原话：

> 「我都不知道你这个逻辑到底是谁前签，还是谁后签，还是没签，
>   还是你后台在签，然后前台没显示刷新出来。」

现在的做法：

1. 前端**逐个账号** `POST /api/checkin?account=xxx`，串行 + 250ms 间隔（保留防风控）
2. 每步写顶部活动条 `#act`：
   - `百度搭子 正在签到…（2/3）`
   - `百度搭子 签到成功，+500 积分`
   - `正在刷新列表…`
   - `完成 · 新签 2，已签 1`（绿色 4 秒后自动收起）
   - 失败转红：`完成 · 新签 0，失败 1（有失败，可下拉刷新重试）`
3. 自动签到（`checkinAll({quiet:true})`）走同一套进度，不弹 toast
4. 页面启动自己就调 `bootAuto()`（内部就是 `checkinAll`），不再只依赖壳层注入
   （壳层注入可能因为页面未加载完而丢失）
5. 统计条右侧加 `ⓘ` 说明入口（`openHelp()`）

> 反面断言在 `TestIndexCheckinShowsProgress` 里：谁改回批量接口，测试就红。
>
> **0.5.5 修订**：说明弹层从五段压到三段（用户：「只写你的签到逻辑就行了，
> 没必要写那么一大堆」）。只保留结论，删掉所有背景解释。

### 3.23 iOS/Android 后台久了「读取失败」的真正原因

不是网络问题，是**渲染进程被系统回收**：

- iOS 后台一段时间后回收 WKWebView 的 WebContent 进程
- Android 同样会回收 WebView renderer

进程死了以后，`evaluateJavaScript` 打进去没有任何反应，页面里的 `fetch`
也全部失败——**下拉刷新救不回来**，因为整个页面进程是死的。

两处修复（必须同时有）：

1. **壳层**（`PointsApp.swift` / `MainActivity.java`）
   - 记录进入后台的时刻
   - 回前台时若离开 > 45 秒 → 整页重建
   - 短时间切换不重建，避免白屏
2. **页面**（`reloadThrottled()`）
   - fetch 连续失败 → 整页 reload，让 WebKit 重建网络栈
   - **60 秒内只允许一次**，否则「失败→reload→又失败→又 reload」会死循环
   - 用 `last_reload_at` 落 localStorage，跨 reload 依然有效

> **0.5.5 修订（重要）**：上面这套在真机上还不够 —— 用户反馈
> 「点击重新加载没用，提示的连接断开正在重建页面也没什么用，只能杀掉 app 重开」。
>
> 原因：`web.reload()` **有时会复用同一个已损坏的渲染进程**，页面依旧是死的。
> 现在改成「先探活，再决定」：
>
> 1. 注入 `window.__pointsRefresh`，看函数还在不在（`'alive'` / `'dead'`）
> 2. 函数不在 → 上下文没了 → 立刻重建
> 3. 函数在 → 3 秒后查 `document.querySelectorAll('.card,.row').length`，
>    还是 0 说明页面没渲染出来 → 重建
> 4. 重建 = **先 `about:blank` 再回面板**，强制 WebKit / Chromium 换一个新进程
>
> Android 侧的 `MainActivity.rebuild()` 和 iOS 侧的
> `WKWebView.reloadFromBlank()` 都是这个逻辑。登录页返回后也走重建
> （原来的 `reload()` 同样可能复用坏进程，表现为「登录完页面空白」）。

### 3.24 面板内联 JS 的语法闸门

模板是一个上千行的 `const indexTmpl = \`...\`` 字符串，**Go 编译器看不见里面的 JS**。
0.5.4 开发中出现过「加函数时误删 `function doCheckin(...)` 的函数头」，
结果是整页白屏（`Unexpected token 'function'`），而 `go test ./...` 全绿——
因为测试只做字符串包含断言。

现在 CI 里有 `面板内联 JS 语法检查` 步骤：

```
go build → 起临时实例 → curl 首页 → tools/check_panel_js.py → node --check
```

本地也能跑：

```bash
curl -s http://127.0.0.1:7979/ -o /tmp/page.html
python3 tools/check_panel_js.py /tmp/page.html
```

> **0.5.5 修订**：这个 CI 步骤一开始写成 `go build ./core/cmd/pointsctl`，
> 在仓库根目录跑会报 `cannot find main module`（根目录不是 Go 模块，`core/` 才是）。
> 已加 `working-directory: core`，脚本路径改用 `$GITHUB_WORKSPACE/tools/...`。

### 3.25 「打开 App 不签到，点按钮才签」的真凶（0.5.5）

用户原话：

> 「aipy 的每日任务奖励在打开 app 的时候不会去领取，百度搭子的也不会检测领奖
>   次数和抽奖，只能点击一键签到才有这个动作。意思是打开 App 的签到和领奖逻辑
>   和一键签到功能一致，相当于打开 App 的时候自动点一次一键签到。」

两个独立的 bug 叠在一起：

**bug 1：待签队列永远为空**

```js
// 错：函数一进来就组装 todo
function checkinAll(){
  var todo = [];
  for (...state.rows...) { if (!r.checked_in) todo.push(r); }
```

而打开 App 的那一刻 `state.rows` **还是空数组**（列表一次都没查过），
于是 `todo.length === 0`，`step()` 立刻返回，直接跳到领奖和刷新。
表现就是「启动时一个都不签，反而点按钮才签」（点按钮时列表早就加载好了）。

修法：把队列组装挪到 `loadStates()` 里，先 `load()` 拿到真实状态再挑未签的。

```js
function loadStates(){
  return load(true).then(function(){
    todo = [];
    for (...) { if (!r.error && !r.checked_in) todo.push(r); }
    total = todo.length;
  });
}
```

**bug 2：启动路径和按钮路径不是同一条**

启动时走的是 `autoCheckin()`，它只 `load` + 挑未签账号，**不领奖、不抽奖**；
点按钮走的是 `checkinAll()`，三件套齐全。同一份 UI 跑出两种结果。

修法：删掉 `autoCheckin()`，启动直接调 `checkinAll({quiet:true})`。
`quiet` 只影响「完事后要不要弹 toast」。

> `TestIndexBootRunsFullCheckinFlow` 钉住这两点：
> 必须存在 `loadStates()`、必须复用 `checkinAll`、必须有 claim/draw 两步。

### 3.26 只有冷启动才自动签（0.5.5）

用户原话：

> 「只有打开应用的时候自动签到，其他切后台等就不要自动签到和领奖了，
>   这样容易黑号，请求太多了。后续让用户自己点击一键签到就行了。」

之前是「每次回前台都签一轮」：冷启动、`visibilitychange`、`pageshow`、
Android `onResume`、iOS `didBecomeActive` 全都会触发。一个下午切十次后台
就是十轮 POST —— 平台风控眼里这就是脚本。

现在的分工：

| 入口 | 行为 |
|---|---|
| 页面启动 `bootAuto()` | **完整一键签到**（签到 + 领奖 + 抽奖） |
| `window.__pointsAutoCheckin` | 同上，壳层冷启动时补一手（`autoTask` 去重） |
| `visibilitychange` / `pageshow` | 只 `refreshOnResume()`，**纯 GET** |
| `window.__pointsRefresh` | 同上，给壳层回前台用 |
| 底部「一键签到」按钮 | 完整一键签到 |

「冷启动」怎么判断：壳层各自维护一个 `bootChecked` 布尔值，
首次 `onResume` / `didBecomeActive` 才是冷启动，之后都算回前台。
（进程被杀后重开 → 布尔值重置 → 又会自动签一次，符合预期。）

页面侧的 `refreshOnResume()` 还兼着自愈职责：如果刚启动就 `load` 失败
（页面出生即残废），限流重建一次。

> `TestIndexResumeDoesNotCheckin` 会扫描 `refreshOnResume()` 函数体，
> 里面出现 `/api/checkin`、`claim_all`、`draw_all` 任何一个就报错。

### 3.27 小额数值不能显示成 0（0.5.5）

`fmt()` 是 `maximumFractionDigits: 1`，用于积分（几百上千）没问题，
但 AiPy 的灵石换算是 `quota / 5000`，0.04 灵石会被格式化成 `0`，
显示成「签到成功，+0 灵石」「今日已签 +0 灵石」—— 看着像签到失败了。

修法：新增两个格式化函数，按数量级自适应。

```js
fmtGain(v)   // >=100 整数；>=1 一位小数；否则两位小数
fmtMoney(n)  // 余额：<1 时保留两位
```

奖励文案（`gainTxt` / `gainPart`）和余额显示都改用它。

### 3.28 任务去重键必须四维（0.6.0）

一开始想去重键只用 `任务类型`，实际会出事：`aipy` 的 `daily_ai_1` 和别的平台
同名任务会互相顶掉——用户签完一个平台，另一个平台的同名奖励就再也领不到了。

正确键是 `日期|平台|账号|任务类型`（`task_memo.go` 的 `taskKey`）。
`TestTaskKeyIsolatesProviderAndAccount` 断言四个维度任一不同即不同键，改实现前先看它。

第二条更要紧：**只有 `Gain > 0` 才记账**。上游经常返回 `ok:true, gain:0`
（任务没达标但也回了 200），这时如果记账，用户当天就再也领不到了 —— 等于假账。

### 3.29 二次核对不能把成功报成失败（0.6.0）

签到接口返回 200 不等于到账：有平台是先受理后排队的。所以 `checkinOne` 在
`res.OK && !res.Already && res.Error==""` 时会再 `Query` 一次确认 `CheckedIn`。

三种边界，写错任何一种都会被用户当成 bug：

| 情况 | 正确做法 |
|---|---|
| 回查拿到 `CheckedIn=true` | 保持成功 |
| 回查拿到 `CheckedIn=false` | 报「未能确认到账」，不写成功（`TestConfirmCheckinDetectsFakeSuccess`） |
| 回查**本身失败**（断网/超时） | **保持成功**，不能判为没到账（`TestConfirmCheckinToleratesQueryError`） |
| 上游说 `Already` | 不回查，省一次请求（`TestConfirmSkipsWhenAlready` 断言 `queryCall == 0`） |

### 3.30 错误分类的顺序是有讲究的（0.6.0）

`Classify()` 先匹配正文关键词、后退回状态码。**关键词表的顺序就是优先级**，不能按字母排：

- `auth` 必须排在 `blocked` 之前：「无权进行此操作」里含「操作」，会被 blocked 的
  「禁止/操作」类词抢走，结果把「登录失效」报成「被风控」，用户跑去重新登录才发现没用。
- `already` 必须排在 `blocked` 之前：「今日已签到」含「签到」，可能被 blocked 的
  「签到时间」子串抢走，把「已签」报成「不在签到时间」。

`TestClassifyOrder` 有 15 个用例守着这张表，动顺序必挂。

另外只有 `network` / `server` / `rate_limit` 三类 `IsRetryable() == true`。
凭证失效（`auth`）和风控（`blocked`）重试多少次都没用，给了重试按钮只会让用户白等。

### 3.31 Webhook 必须在 Go 侧发（0.6.0）

最初想在 JS 里 fetch 过去，两个硬伤：

1. 页面跑在 `127.0.0.1:7879`，`fetch` 会带 `Origin: http://127.0.0.1:7879`，
   企业微信/钉钉网关对这个 Origin 直接拒收；
2. App 切后台时 WebView 的 JS 请求会被系统掐断，签到后半段推送直接丢。

所以前端只做「把本轮结果整理成 `accounts[]` + `summary`」，发送、脱敏、重试计数全在
`notify.go`。脱敏必须在**发送前**做：Webhook 是把内容推出设备，cookie/key 一旦进去
等于发给第三方服务器留档（`maskNotify` 覆盖 `sk-` / `cookie:` / `authorization:` /
`bearer ` / 11 位手机号 / 邮箱 / `session=`）。

### 3.32 删除令牌时的「在用」保护（0.6.0）

用户可能把 `sk-xxx` 直接当账号登录凭证用。这时在令牌列表里删掉它，账号立刻 401，
而用户完全不理解发生了什么（「我就删了个没用的 key」）。

处理：`handleTokenDelete` 比对 `in.Key == cred.AccessToken`，命中就拒绝并返回
`in_use:true`，前端弹一次明确说明后果的确认框，确认后才带 `force:true` 重发。
用户原话是「无所谓，你看着定」——这个保护是必要的，因为它防的是不可逆的误操作。

### 3.33 通知的三个开关别搞混（0.6.0）

`on_success` / `on_failure` / `always` 是**结果类型**维度，
`on_manual` / `on_start` 是**触发源**维度，两组正交。

一开始只有前两个，结果「全部已签的一轮」在 `on_failure=true, on_success=true` 下
因为 `hasFail=false` 走 `on_success` 分支而被推出去 —— 用户收到的是一条
「新签 0，已签 4」的推送，觉得吵。`always` 就是给这种场景用的显式开关：
不勾时按结果类型过滤，勾了才无条件推。

> **0.6.7 起这些开关已从界面全删**，固定为「启用即推」：上面两组维度只作历史背景保留，
> 读代码时别再以为它们还会影响推送。`send()` / `save()` 里会把 `OnStart/OnManual/Always/OnSuccess/OnFailure`
> 一律归一化成 true，读旧配置也拦不住推送。改通知逻辑前先读 `notify.go` 的 `send()` 头注释。

### 3.37 iOS 长时间后台回前台「整体卡顿」（0.7.2 定论）

**症状**：切后台放着，几小时后回前台，整个界面发木——点一下要等半天才响应。

**不是前端慢，也不是引擎慢。** 压测过：本地接口 0.0003s，50 并发 `Dispatch` 只要 162µs，
Go 侧锁竞争为零；前端也没有定时器堆积。真正原因在 WebKit 的进程挂起机制，两层叠加：

1. iOS 在 App 即将挂起时调 `ProcessStateMonitor::processWillBeSuspendedImmediately()`
   → `WebProcessPool::setProcessesShouldSuspend(true)` → `ProcessThrottler::setAllowsActivities(false)`。
   此后页面**不接受任何前台活动**，绘制区还会 `hideContentUntilPendingUpdate()` 清空图层树。
2. **解除挂起依赖 RunningBoard 的异步回调**，回前台瞬间还没生效。而原来
   `didBecomeActive` 里立刻 `evaluateJavaScript`，它底层是
   `runJavaScriptInFrameInScriptWorld`——正好在拒绝名单里。

WebKit [PR #66838](https://github.com/WebKit/WebKit/pull/66838) 的日志原文：

> the log showed `setProcessesShouldSuspend(1)` / `setAllowsActivities 0` followed by repeated
> **"not allowed to add foreground activity WebPageProxy::runJavaScriptInFrameInScriptWorld"**

交互请求被一条条拒掉再排队，用户看到的就是「点一下反应半天」。

**另一层**：[Bug 321511](https://bugs.webkit.org/show_bug.cgi?id=321511) 记录了进程被**挂起**
（而非 kill）的情形——`webViewWebContentProcessDidTerminate` **不会触发**，
页面就此僵死。我们之前完全没实现这个回调，等于没有任何兜底。

**0.7.0 的修法是错的**（保留在此作为反面记录）：当时以为只要延后 350ms 就能避开
`setAllowsActivities(false)` 的窗口。用户实测锁屏 4 分钟回来，界面**完全没反应**——
说明那不是「活动被拒绝」，而是进程冻结后压根没恢复。

**0.7.1 的真正修法**：
- 回前台**先探活**：离开 ≥1.5s 就发 `1+1`，700ms 内没回来即判定冻结。
  冻结时 `completionHandler` 永远不执行，这是唯一可靠的判据。
- 两级重建：先 `load()`；1.2s 未完成则改 `.id()` 销毁重建 WKWebView
  （进程池整体卡死时唯一能自救的手段）。
- 实现 `webViewWebContentProcessDidTerminate`，自动重载 `PanelSchemeHandler.baseURL`。
  **带 60s 冷却**：进程反复崩溃时无限 reload 会一直闪白，比不恢复还糟。
  重载后会走 `bootCheck()` → `/api/daily` 判定，今天跑过就只刷新，**不会重复签到**。
- 顺带降 GPU 常驻开销：`.appbar`/`.bar` 的 `backdrop-filter` 从 `blur(18px)` 压到 `blur(10px)`
  并加 `@supports` 降级（一个 sticky 一个 fixed，长期停在屏幕上，每帧都要重算模糊）；
  删掉 `.card` 的 `will-change:transform`（每张卡一个独立合成层，账号多了反而拖慢合成）。

> ⚠️ **别再动这几处**。`core/panel/perf_test.go` 有回归锁会直接失败。
> 另外 `inactiveSchedulingPolicy` 在这条路上**用不上**：它是 iOS 17+ 可用的 API，
> 但只管「web view 不在任何 window 里」的情形，我们的 WebView 始终在窗口内。

#### 0.7.2：0.7.1 的「探活 + 两级重建」也失败了

用户实测 0.7.1：**锁屏 4 分钟回来，卡顿恢复不了，一直卡。**

复盘出两个致命点，都值得记住：

1. **探活的判据不可靠。** `evaluateJavaScript("1+1")` 能回话，只证明 JS 引擎还在跑，
   **不证明输入事件能被处理**。WebView 卡死时最常见的是「脚本活着、触摸全吞」——
   这时探针返回 2，代码判定「健康」，于是只刷新不重建，**把自己锁死在卡死状态**。
   用「能不能执行 JS」去推断「能不能响应点击」，这个假设本身是错的。
2. **恢复链路太长。** 0.7.1 是「先 `load()` → 1.2s 没加载完 → 再换 `.id()`」，
   一次恢复跨两个 runloop、三个 `asyncAfter`。任何一个没排上（主线程一忙就排不上），
   链路就断在半路，而 `recovering` 还停在 `true`，后续所有回前台事件都被
   `if recovering { return }` 挡掉 —— 于是「一直卡，恢复不了」。

**0.7.2 的做法：放弃判断。**

| 离开时长 | 动作 |
|---|---|
| < 1.5s | 只调 `window.__pointsRefresh()` |
| 1.5s ~ 10s | 探一次活，800ms 不回就 `hardReset()`（这一档是为了不打断正在跑的签到） |
| **≥ 10s** | **直接 `hardReset()`**，不问进程死活 |

`hardReset()` 在**一个 runloop 内**完成：断开旧实例 `navigationDelegate`（防止它的延迟回调
污染新状态）→ `webView = nil`（立刻放掉强引用）→ 换 `webGen`（SwiftUI 建全新实例）。
**不做 `load()`**：旧实例已经不处理消息，load 也是白发。

3 秒没 `didFinish` → 显示原生重试页（`stalled` 状态，「页面没有响应 / 重新加载」），
用户不用杀进程重开。

顺带压掉两处长期开销：
- `isInspectable` 收进 `#if DEBUG`（release 下它让 WebKit 常驻保留调试元信息）；
- `isOpaque = true`（原来是 clear，透明会让 WebView 每帧与父视图重新合成）；
- `StatusBarStyle.dark` 去掉 `@Published` 改手动同值短路——页面每次 `render()` 都可能
  postMessage 一次 `setTheme`，原来会让根视图和 `preferredColorScheme` 反复重算。

**还有一个必须同时看的**：`PanelSchemeHandler` 加了并发闸门（`DispatchSemaphore(value: 8)`）
和请求级超时（普通 20s / 签到类 180s，超时回 504）。原因：Go 导出函数同步阻塞，
一个请求占一条 GCD 线程；后台积压的请求回前台一股脑发出去会把 global 队列占满，
后续接口全排队 —— 表现同样是「点哪都没反应」。超时兜底则防止「Go 卡在没设超时的地方」
让页面永远转圈。两者都用 `OnceFlag` 保证只结算一次。

### 3.38 图标「不清晰」和左上角 logo「不更新」（0.7.0）

用户反馈两次，前三次判断都错了（怀疑圆角被二次裁切、怀疑图形占比太小、怀疑空心描边太细）。
**教训：必须提取实物、同尺寸逐像素比对已知正确的实现，不能凭经验推断。**

把 0.6.9 的真机截图和 `aipy-checkin` 的图标放到同一渲染尺寸（137px）逐像素量：

| 指标 | AiPy | 我们 0.6.9 |
|---|---|---|
| 白色描边占画布 | 7.3% | 13.9% |
| 同条件虚边比例 | 16.5% | 16.5% |

结论：
- **虚边比例完全一样**，所以「超采样」「LANCZOS 逐级缩」「去 alpha」都**不解决清晰度**。
  实测 `1024→180 一步缩` 12.7%，`2048 逐级缩` 13.8%，反而略差。
- 真正导致观感糊的是**描边太粗（1.9 倍）**：主星四角之间的凹陷被填平，星形退化成一团。
  但**用户明确拒绝改描边**，所以 0.7.0 保留 48/28 不动。
- **左上角 logo 不更新是另一个 bug**：`index.go` 的 `ICON('spark')` 是 24×24 **实心**路径，
  和 App 图标的空心双星是两套独立图形，改 App 图标永远影响不到它。
  0.7.0 已改为按 1024 坐标等比映射到 24 网格（主星 1.13 / 副星 0.66）。
  注意 `iconSVG()` 外层原本写死 `stroke-width="1.8"`，会把 path 自带的线宽覆盖掉，
  所以 spark 单独走一条不带外层线宽的分支。

图标产物另有两条硬规范（以前都没满足）：
- **不能预烘焙圆角**。iOS 自己按 ≈22.37% 裁；预烘焙后圆角外侧的透明像素在部分渲染路径
  （第三方商店 / 重签安装）会被填成**纯黑**，边缘发脏。
- **不能有 alpha 通道**。App Store 强制要求，透明区渲染成纯黑。
  0.6.9 的 `app-icon-512.png` 有 47831 个全透明像素。

现在统一由 `tools/gen_icon.py` 产出，`python3 tools/gen_icon.py --check` 可校验合规。

#### 0.7.2：以上全部不是「糊」的原因，真因是 Info.plist 声明错了

用户 0.7.1 实测：「图标还是那样模糊」。

把 IPA 拆开看 `Info.plist`：

```
CFBundleIconFiles = ["AppIcon60", "AppIcon120", "AppIcon180"]
.app 根目录        = AppIcon60.png(60) AppIcon120.png(120) AppIcon180.png(180)
CFBundleIconName   = 不存在
```

**iOS 解析 `CFBundleIconFiles` 的规则是「基础名 + @2x/@3x」。** 对 `AppIcon60`
它只会找 `AppIcon60.png` / `AppIcon60@2x.png` / `AppIcon60@3x.png`：

| 系统要找 | 我们有的 | 结果 |
|---|---|---|
| `AppIcon60.png`（1x=60） | ✅ 有 | 用得上 |
| `AppIcon60@2x.png`（2x=120） | ❌ 没有（有 `AppIcon120.png`，但那是**另一个基础名**） | 找不到 |
| `AppIcon60@3x.png`（3x=180） | ❌ 没有（同理） | 找不到 |

所有现代 iPhone 是 @3x 屏，要 180px 的主屏图标，系统只找到 60px 那张 →
**放大 3 倍**铺上去。源图画得再锐利，主屏上也一定是糊的。

而 `CFBundleIconName` 从来没写过，所以 `actool` 编译好的 `Assets.car`
（13 尺寸、RGB 无 alpha、满幅不预圆角）**一次都没被用过**。

这解释了为什么 0.7.0、0.7.1 连续两版改图都「没变化」——iOS 根本没在看新图。

**修法（`build-ipa.sh` 第 4 段已重写）：**

1. `CFBundleIconName = AppIcon` → 系统读 `Assets.car`（首选路径）；
   只有 `Assets.car` 真的存在才写，否则会变成空白图标；
2. 平铺文件按 `AppIcon60@2x.png` / `AppIcon60@3x.png` / `AppIcon76.png` /
   `AppIcon76@2x.png` / `AppIcon83.5@2x.png` 正确命名，兜住「自签重签工具破坏
   `Assets.car`」的情况；
3. `CFBundleIconFiles` 只列**基础名**（`AppIcon60` / `AppIcon76` / `AppIcon1024`），
   让系统自己拼后缀。

回归锁在 `core/panel/icon_plist_test.go`：`TestIconDeclaredFromAssetCatalog`、
`TestIconFlatFilesUseScaleSuffix`、`TestIconFilesListUsesBaseNamesOnly`。

> **教训（第二次踩同一类坑）**：判断「图糊」之前，先确认**系统实际在用哪张图**。
> 拆 IPA 读 `Info.plist` + 列 `.app` 根目录文件名，两步就能定位，比对着源图反复量
> 像素有效得多。前两轮全部在「图本身」这一层打转，压根没验证「图有没有被用上」。

---

## 4. 各平台实现要点

| 平台 | ID | 鉴权 | 移动端 | 特有能力 | 源码 |
|---|---|---|---|---|---|
| WorkBuddy | `workbuddy` | access token（官方 OAuth） | ✅ | — | `workbuddy.go` |
| 金山灵犀 | `lingxi` | cookie | ✅ | — | `lingxi.go` |
| 百度搭子 | `dumate` | cookie | ✅ | **抽奖** | `dumate.go` |
| Trae | `trae` | token | ✅ | — | `trae.go` |
| AiPy | `aipy` | cookie | ✅ | **任务领取** + **API 令牌** | `aipy.go` |

### 4.1 百度搭子接口清单

```
GET  /api/dumate/points/loginBonusInfo        签到状态（hasIssued / totalPoints）
POST /api/dumate/points/loginBonus            领每日积分（body 必须带 request_id=UUID）
GET  /api/dumate/points/quota_overview        积分总览（必须带 clientType & timezone）
GET  /api/dumate/activity/growth-plan/draw/status    剩余抽奖次数（remaining_draws）
POST /api/dumate/activity/growth-plan/draw           抽一次（body request_id=UUID）
POST /api/dumate/activity/growth-plan/prize/claim    领实物奖（积分奖自动到账，一般不用调）
```

### 4.2 WorkBuddy OAuth 流程

网页登录拿不到 access token，必须走官方 OAuth：

```
POST /v2/plugin/auth/state  → 拿到 authUrl
   ↓ 用户在 WebView 里授权
轮询 /v2/plugin/auth/token  → 拿到 access token
```

面板里 `/login/workbuddy` 是本机 302 端点，实时换 authUrl 再跳转。

### 4.3 加一个新平台要改哪些地方（清单）

1. `core/provider/你的平台.go`——实现 `Meta/Query/Checkin`，可选 `TaskClaimer`/`LotteryProvider`，末尾加 `var _ Provider = (*Xxx)(nil)`
2. `core/provider/registry.go`——在 `NewRegistry` 里 `reg.add(NewXxx(c))`
3. `core/mobile/mobile.go`——`loginURLs` 加登录入口
4. `core/provider/xxx_test.go`——用 `httptest` 写契约测试（路径、方法、必需头）
5. `core/provider/provider_test.go` 的 `TestRegistryHasCore`——`want` 列表加 ID（这个测试故意让你别忘了改）

**不需要**改：`panel/`、`mobile` 导出函数、iOS/Android 原生代码。UI 会自动出现新卡片。

---

## 5. 工作流

### 5.1 推代码（★ 沙箱里 git 协议不通）

`git push` 会卡 130 秒后报 `Failed to connect to github.com port 443`。必须用 Git Data API：

```bash
export GH_TOKEN=<PAT>
python3 tools/gh_sync.py --repo ElyYinLi/aipoints-mobile --dry-run   # 先看差异
python3 tools/gh_sync.py --repo ElyYinLi/aipoints-mobile --message "feat: xxx"
# 等价快捷方式：MSG="feat: xxx" GITHUB_TOKEN=<PAT> bash tools/push-via-api.sh
```

脚本会：用 blob sha 比对本地与远端 → 只给有差异的文件建 blob → 带 `base_tree` 建 tree → 建 commit → 移动 `main` 引用（`force:false`，只允许快进）。

三条必须记住的：

- **`base_tree` 不能省**。缺了它等于按本地文件重建整棵树，远端有、本地没有的文件会被静默删除。0.6.0 推送时旧脚本就踩在这个点上，已改为薄封装。
- **mode 从 git 读，不靠后缀猜**。按 `.sh` 结尾判 100755 会把非 `.sh` 的可执行文件降成 100644，推上去就丢了可执行位，`git status` 还看不出来。
- **推的是工作区**（已跟踪 + 未忽略的未跟踪文件），不需要先 `git commit`。这是与旧脚本的语义差别：旧的要先 commit。

### 5.2 提交流程

```bash
cd core && gofmt -w . && go vet ./... && go test ./...     # 必须全绿
cd .. && python3 tools/check_secrets.py                     # 扫密钥
git add -A && git commit          # 本地留档，便于回溯
python3 tools/gh_sync.py --repo ElyYinLi/aipoints-mobile --message "feat: xxx"
```

### 5.3 CI（三个工作流，push 到 main 自动跑）

| 工作流 | 触发 | 产物 |
|---|---|---|
| `core-ci` | `core/**` 变更 | Go 测试 + vet |
| `build-android` | `core/**`、`android/**` | `Points.apk`（Actions 产物，7 天） |
| `build-ios` | `core/**`、`ios/**` | `Points.ipa`（Actions 产物，7 天） |

### 5.4 Release 通道（发布用）

| Tag | 用途 | 更新方式 |
|---|---|---|
| `android-latest` | Android 滚动直链 `/releases/download/android-latest/Points.apk` | 手动 `gh release upload --clobber` |
| `ios-latest` | iOS 滚动直链 | `workflow_dispatch` + `make_ipa=true` 时 CI 自动上传 |
| `v0.5.x` | 版本归档（双端附件） | 发版时新建 |

**为什么要有 Release**：Actions 产物要登录才能下；仓库是公开的，挂 Release 后手机浏览器直接点链接就能下。

### 5.5 签名（★ 稳定可覆盖安装，不能丢）

- 私钥：`android/signing/points.jks`（**本地保留，`.gitignore` 已排除，绝不入库**）
- CI 从 Actions Secret `POINTS_KEYSTORE_B64` 还原
- 当前签名指纹前缀 `ef6e16c9…`，与 0.4.x 一致 → 可覆盖安装、不丢账号
- **换签名 = 必须卸载重装 = 丢所有账号**。任何涉及签名的改动都要极度小心

### 5.6 版本号要改四处

| 文件 | 字段 |
|---|---|
| `core/mobile/mobile.go` | `var Version = "0.6.9"` |
| `android/app/src/main/AndroidManifest.xml` | `versionCode` / `versionName`（0.6.9 → 24 / 0.6.9，code 每次必须 +1，否则覆盖安装会被拒） |
| `ios/App/Info.plist` | `CFBundleShortVersionString` 0.6.9 / `CFBundleVersion` 24 |
| `README.md` | version badge `version-0.6.9` |

漏改一处的症状：设置页显示旧版本号，或覆盖安装被系统拒绝（versionCode 没 +1）。

---

## 6. 界面代码导航（`core/panel/index.go`）

这文件 3100 行，全部 UI 都在这。结构：

```
<style> ...设计令牌 / 组件样式... </style>     ← 约 1-550 行
   :root 里是 Material 3 色调令牌，改主题改这里
   日志样式在「/* ---- 运行日志 ---- */」

<body> 骨架                                     ← 约 550-650 行
   .hero（合计区）→ #chips（统计条）→ #list（账号卡片）→ 底部按钮

<script>                                        ← 约 650 行之后
   ICON / fmt / el / toast / j / post 等工具
   render()            渲染账号列表（按 state.view 决定卡片还是紧凑行）
   doCheckin()         单账号签到
   checkinAll()        一键签到：loadStates → 逐个签 → 领任务 → 抽奖 → 刷新
   loadStates()        先读一遍状态再组装待签队列（0.5.5 修复，别再用空 state.rows）
   bootAuto()          冷启动入口 = checkinAll({quiet:true})，壳层也调它
   refreshOnResume()   回前台只刷新，不签到（+ 失败时自愈重建）
   actShow/actDone/actErr  顶部活动条的三种状态
   fmtGain/fmtMoney    小额数值格式化（灵石 0.04 不能显示成 0）
   summaryOf()         一键签到结果压成一句话
   pushSummary()       一键签到收尾把本轮结果交给 Go 侧推送（不 await）
   openSheet()         通用底部弹层
   openSettings()      设置面板（外观 / 通知 / 账号管理 / 备份 / ★ 运行日志）
   openNotify()        签到通知配置（五渠道 + 四个触发开关 + 发测试）
   openAccountMenu()   卡片「···」菜单：任务 / 抽奖 / 令牌 / 启停 / 删除
   openTokens()        API 令牌列表（含「在用」标记与删除保护确认）
   openTokenEdit()     新建 / 编辑令牌（额度、模型限制、IP 白名单）
   showNewKey()        创建成功后单独弹一次完整 key（只此一次机会）
   openLottery()       抽奖面板
   openTasks()         任务面板
   beginTrack()        长按拖拽排序
   applyView()         卡片/紧凑视图切换
```

### 6.1 图标怎么加

1. `core/panel/icons.go` 里加 SVG，键名注册到表里
2. HTML 里写 `{{ICON:name}}`，Go 渲染时替换
3. 需要 JS 动态用时，`ICON('name')` 直接取

**App 桌面图标（0.6.9 V4 错位呼应）**：必须与主界面左上角 logo 同源——深蓝紫渐变圆角底 + 白色 **spark 双星空心描边**。V4 把小星半径放大、描边减细到 28（用户红线：**小星不许加粗，否则看不到空心**）。四份文件共用同一组 1024 画布坐标（主星 `M455,195...` / 副星 `M710,623...`），改一处就要全改：

| 文件 | 用途 |
|---|---|
| `core/panel/icons.go` 的 `appIcon` | SVG 源（Go 常量） |
| `assets/app-icon.svg` | SVG 源（独立文件） |
| `assets/app-icon-512.png` / `app-icon-small.png` | iOS 构建取用（由 SVG 用 cairosvg 渲染，1024 / 540） |
| `android/app/src/main/res/drawable/ic_launcher.xml` | Android VectorDrawable（同坐标转写） |

有 `icons_test.go` 钉住三端坐标一致（含 V4 的 `M455,195` / `M710,623` / `stroke-width="28"`），改完跑 `go test ./panel/`。

### 6.2 加新平台不用改这个文件

信息全来自 `/api/providers` 的 `Meta`。只要 provider 实现正确，卡片自动出现。

### 6.3 改 UI 后的验证方式

```bash
cd core && go run ./cmd/pointsctl -action serve    # 起面板
# 浏览器开 http://127.0.0.1:7879
```

更严谨的做法（本项目一直在用）：Playwright 打桩 `fetch` 造数据 + 截图看渲染。

### 6.4 内联 JS 的语法闸门（0.5.4 起，每次改 index.go 必跑）

`index.go` 是一整个 Go 字符串，Go 编译器不会看里面的 JS。写错一个括号照样能编译，
到手机上才白屏。所以有一道独立闸门：

```bash
# 先把渲染后的 HTML 导出来（panel_test.go 里有现成的 DUMP 写法，或临时加一个
# 只写文件的测试），再交给 node --check
python3 tools/check_panel_js.py /tmp/index_rendered.html
# 输出：内联脚本 2523 行 / 96845 字节 → 内联 JS 语法检查通过
```

`TestIndexRendersSelfContained` 会额外拦三类问题，改 UI 时容易被它挡下：

- 残留 `{{ICON:xxx}}` 占位符（`buildIndex` 的白名单漏了这个图标名）
- 出现 `http://` / `https://` / `@import url(` —— 面板必须自包含，任何外部资源都不许引
  （连 placeholder 里的 `https://qyapi...` 也会被判违规，只能写成不带协议头的域名）
- 缺少 `__pointsAutoCheckin` / `PointsBridge` / `points://login` / `pullToRefresh` / `safe-area-inset`

---

## 6.5 令牌与通知（0.6.0，从 aipy-checkin 移植）

### 接口一览

```
GET  /api/tokens?provider=aipy&account=<id>   → {ok, tokens[], in_use_key, usable}
POST /api/tokens/save    {provider, account, token:{id?,name,remain_quota,expired_time,unlimited_quota,model_limits,allow_ips}}
POST /api/tokens/delete  {provider, account, id, key, force}
POST /api/tokens/status  {provider, account, id, status}       // 1 启用 / 2 禁用

GET  /api/notify/config  → {ok, config, types, fail_streak, last_error, disabled}
POST /api/notify/config  {enabled,type,url,on_success,on_failure,on_manual,on_start,always}
POST /api/notify/test    {}                                    // 走与正式推送完全相同的路径
POST /api/notify/reset   {}                                    // 清「已自动停用」标记
POST /api/notify         {trigger, accounts[], summary}         // 前端上报本轮结果
```

### 为什么 `/api/notify` 永远返回 `ok:true`

推送失败绝不该让前端以为签到失败了。所以它只回 `{ok:true, sent:bool, error?}`，
前端拿到 `error` 只是 `console.warn`，不弹 toast。改这个行为前先想清楚：
用户点「一键签到」看到「推送失败」会以为签到失败了。

### 触发开关的语义（别把三个搞混）

| 字段 | 含义 |
|---|---|
| `on_success` | 有账号签到**成功**时推 |
| `on_failure` | 有账号签到**失败**时推（失败项在正文里置顶） |
| `always` | 一轮跑完就推，**不看结果**（含「全部已签」「全部失败」） |

`on_start` / `on_manual` 是**触发源**开关，和上面三个是正交的：先按触发源过滤，
再按结果类型过滤，`always` 命中则跳过结果类型判断。

> **历史（0.6.7 已废弃）**：上表只是字段含义备查。0.6.7 用户拍板删掉整个「什么时候推」区块，
> 这五个开关 + `on_start/on_manual` 全部不再参与判断——通知启用即推。`NotifyConfig` 里仍保留这些字段
> 仅为存量配置兼容，`save()` / `newNotifier()` 会把它们强制置 true。别照着这张表去「恢复开关逻辑」。

### 落盘位置

`DataDir` 下三个文件，全部 `0600` + 原子写（`.tmp` → `rename`）：

| 文件 | 内容 |
|---|---|
| `task_memo.json` | 今日已领过的任务键（加载时自动丢弃非今日的） |
| `notify_memo.json` | 通知配置（**含 Webhook URL**，URL 里有机器人 key，等同凭据） |
| `gain_memo.json` | 签到奖励兜底（0.5.2 起，用于「今日已签 +N」） |

---

## 7. 运行日志（0.5.1 新增，排查问题第一入口）

### 7.1 用户侧

**设置 → 运行日志**：时间 / 方法+路径 / 状态码+业务码 / 消息，可刷新、清空、一键复制。

### 7.2 接口

```
GET  /api/logs?limit=200   → {ok, count, logs:[{time,date,level,scope,method,path,status,cost_ms,msg,code,resp}]}
POST /api/logs/clear       → {ok}
```

`logs` 是**最新在前**。另有 `mobile.Logs(limit)` 供原生壳兜底。

### 7.3 三条设计约定（改之前想清楚）

1. **只记异常**。成功请求不记——否则一次下拉刷新几十条，真问题被埋
2. **凭据一律脱敏**。`httpx/log.go` 的 `maskText()` 盖掉 `cookie`/`authorization`/`token`/`BDUSS`/`stoken`/`bce-user-info`/32 位以上连续 hex
3. **主动取消不记**。连续下拉刷新导致的 `context.Canceled` 属正常交互

容量 400 条环形缓冲。**加新日志点前先问自己：这条日志在用户报障时有用吗？**没用就别加。

---

## 8. 账号存储与排序

`store.FileStore` 写 `{dataDir}/accounts.json`：

```json
[{"provider":"dumate","account_id":"xxx","label":"小号","enabled":true,
  "secret":{...},"order":1}]
```

- `Order` 是拖拽排序位置；**没有这个字段时按添加顺序自然排列**，不需要数据迁移
- `Put()` 覆盖已有账号时若 `Order==0` 会**保留原位置**；新账号 `maxOrder+1`
- `Reorder(keys)` 按数组下标写 `Order`，未命中的接尾保持相对序，最后 `sort.SliceStable` 落库

移动端凭据另外进 iOS Keychain / Android Keystore。

---

## 9. 设计约束（别破坏这些）

| 约束 | 原因 |
|---|---|
| 引擎只监听 `127.0.0.1` | 凭据不出本机，不做任何第三方中转 |
| 密码只流向平台登录页 | 我们不做账号密码表单，平台改登录流程不用跟改 |
| 前端零框架零打包器 | 包体为零、首屏无白屏 |
| UI 单一来源（Go 侧 HTML） | 两端完全一致，改一次生效 |
| 写请求串行 + 1.2s 间隔 | 同设备批量并发最容易触发风控 |
| 只有真发过写请求才 sleep | 登录失效的账号白等会让「一键签到」显得很卡 |
| 备份是明文 JSON | 会提示用户别外发；文件由系统文件面板选位置 |

---

## 10. 常用命令

```bash
# 测试 / 检查
cd core && go test ./... && go vet ./... && gofmt -l .
cd core && go test ./provider/ -run TestDumate -v     # 只跑某组

# 本地起面板看界面
cd core && go run ./cmd/pointsctl -action serve -dir /tmp/pdata
# → http://127.0.0.1:7879

# 命令行查/签（不经过 UI）
cd core && go run ./cmd/pointsctl -action query -provider dumate -cookie "BDUSS=..."

# 推代码
python3 tools/gh_sync.py --repo ElyYinLi/aipoints-mobile --message "feat: xxx"

# 密钥扫描
python3 tools/check_secrets.py
```

---

## 11. 待办与已知限制

- **`check_secrets.py` 会误报** `android/signing/points.jks`（本地存在但已被 `.gitignore` 排除、未跟踪）。这是脚本对本地文件的误报，不影响推送；要更严谨可让它只扫 `git ls-files` 的内容
- **iOS 不是 Xcode 工程**，`build-ios.sh` 手写编译。要加原生依赖会麻烦
- **AES/加解密**：移动端凭据靠系统 Keychain/Keystore，Go 侧不做加解密
- **抽奖历史只在内存**，App 重启即清空（用户没提需求，先这样）
- **沙箱里 GitHub 只能走 REST API**，见 5.1
- **令牌列表的 key 只有首尾几位**：上游列表接口做脱敏，完整 key 仅在创建响应里出现一次。
  这不是我们的取舍，是平台就这样，所以「新建后立刻复制」那一步不能省
- **通知只有一个 Webhook**：多个渠道要分别配置时只能来回改地址。用户明确说「全部 5 个」
  指的是渠道种类，不是同时推 5 个地方
- **令牌管理只有 AiPy**：`TokenManager` 接口是通用的，但其余平台控制台没有对应接口
- **`always` 开关默认关闭**：打开后每次冷启动都会推一条，一天开 5 次 App 就 5 条
- **卡顿的最终手段仍是「换实例」**：`hardReset()` 能解决 WebContent 进程僵死、触摸被吞、
  图层缓存损坏等绝大多数情况，代价是页面状态（滚动位置、展开的面板）重置。
  如果 0.7.2 在真机上仍卡，下一步要看的是「重建有没有真的发生」——`stalled` 重试页
  是否出现过、点「重新加载」能否恢复，这两个现象能区分「重建没跑」和「重建也救不回」
- **`Assets.car` 与平铺图标两条路并存**：自签重签工具（SideStore / Sideloadly）有时会
  破坏 `Assets.car`，所以平铺 PNG 必须一直保持正确的 `@2x/@3x` 命名，别图省事删掉

---

## 12. 版本历史

| 版本 | 要点 |
|---|---|
| 0.4.x | 打通 Android 零 Gradle 构建链、AiPy 一键领任务、iOS 前台恢复 reload |
| 0.4.2 | 一键签到合并领任务+抽奖、主题切换、状态栏沉浸、备份文件导入导出 |
| 0.5.0 | 改名**积分矩阵**、抽奖可视化入口、账号拖拽排序、卡片/紧凑双视图、README 重写、加 `v0.5.0` tag |
| 0.5.1 | 修 410110（UUID）、签到 +N 回执（差值反推）、提示统一带单位、设置页运行日志 |
| 0.5.2 | 状态行统一「今日已签 +N 积分」（gain memo 兜底）、删掉合计区解释文案、system 主题改 JS 判定修 Android WebView 不跟随 |
| 0.5.3 | 「今日已签」不再有数字时显示空、彻底删掉「跟随系统」主题 |
| 0.5.4 | 签到进度条逐账号可见、iOS/Android 后台假死改整页重建、内联 JS 语法闸门 |
| 0.5.5 | 冷启动走完整一键签到（修 todo 为空）、回前台只刷不签、说明弹层压到三条 |
| **0.6.0** | **从 aipy-checkin 移植：API 令牌管理（AiPy）+ 五渠道 Webhook 通知；规则加固：任务去重键、busy 互斥、签到二次核对、ErrKind 错误分类；卡片次级功能收进「···」菜单** |
| **0.6.1** | **修：令牌列表显示完整 key + 复制、新建被 usable 误判挡死、去掉编辑；任务/抽奖放回卡片与紧凑行（只有令牌进「···」）；修全部已签时推送不发** |
| **0.6.3** | **修三处实测**：① 推文时间差 8 小时——Go 在手机上取不到系统时区，改为由前端带 tz_offset 换算（无偏移时显式标 UTC）；② 运行日志默认显示全部级别并加「全部/只看异常」切换，推送的发送·跳过·失败每次都留痕，签到每轮开始也留痕；③ 通知设置新增「重发上次结果」（force，绕过开关），另：紧凑行不再渲染 ··· 令牌入口（挤在任务与签到之间像多余的点），卡片视图菜单仍可进 |
| **0.6.2** | **UI 重排**：「···」菜单只留 API 令牌（任务/抽奖本来就在卡片上）；卡片余额移到标题行右侧，消除左侧 48px 空白带；卡片/紧凑行的任务·抽奖按钮去掉图标只留文字；紧凑行名称不再重复拼接平台名、余额并入副标题，三行基准对齐；令牌 key 完整显示（原来还在 maskKey 脱敏）、复制按钮并入「复制/禁用/删除」一行；令牌说明条改中性色；到期时间兼容毫秒时间戳 |
| **0.6.4** | **启动逻辑重做 + 大扫除**：①「···」全删（卡片右上、紧凑行），账号管理/令牌改为 设置→账号→点账号；② 六个自动触发点全删（页面 bootAuto、Android/iOS 首启注入、两端 45s rebuild、两端探活重建），另删 Android 前台服务/开机自启与 iOS BGAppRefreshTask（后台偷偷签到）；③ 打开 App 改为「今日判定」：引擎落盘 daily_memo.json，今天跑过就只刷新 + 提示哪几个没签，没跑过才走完整流程，**一键签到按钮逻辑未动**；④ 回前台兜底全删（reloadThrottled、j() 自动重试、45s 阈值、DOM 探活），真因修为 HTTP 短连接（DisableKeepAlives）+ load 改 allSettled 保旧数据；⑤ 日志对标 aipy-checkin：毫秒时间戳、每轮共用 #轮次号、按步骤记（签到/核对/领任务/抽奖/推送）成功也记、缓冲 1500、前端 app 日志入同一条时间线；⑥ 设置改四页签 通知/账号/数据/日志，删除外观分区 |
| **0.6.5** | 通知渠道 5 标签的背景圆角从胶囊改小（12/9px）；账号管理从「点进详情」改为行内按钮（令牌/停用/删除）+ 底部小字说明，详情页整段删除 |
| **0.6.6** | ① 日志时间差 8 小时根治：前端每个请求带 `X-TZ-Offset`，服务端据此换算日志时间戳；② 删三处占空间提示语（账号/数据/日志页）；③ 账号名长按改备注（500ms，加 `POST /api/account/label`）；④ 添加账号页删「手动粘贴/导入备份」与「关闭」按钮 |
| **0.6.7** | ① 通知「什么时候推」四个触发开关 + 「重发上次」全删，固定「启用即推」，存量配置强制归一化；② 各页签底部版本号/平台数与关闭按钮、设置页「设置」标题全删；③ 版本号迁到「签到逻辑」页底部（同时填 `state.meta` 供日志导出）；④ 设置/签到逻辑弹层用 `hideTitle`/`hideCancel` |
| **0.6.8** | ① **App 图标改为左上角同款 spark 双星**（渐变底 + 白星，四文件同源，加 `icons_test.go`）；② 账号页令牌说明删「只有支持令牌的平台才有这个按钮」；③ 数据页导出按钮合成一行 生成/复制/导出 |
| **0.6.9** | ① **iOS 后台「读取失败」根治（方案 A2）**：主文档 + 全部 API 走 `pointsapi://panel/`，`WKURLSchemeHandler` → `MobilePanelDispatch` → `panel.Server.Dispatch` 进程内分发，零 TCP socket；前端 `j()` 给 POST 加 `X-Points-Body` 兜底头；本机 server 只留 OAuth 302 / `pointsctl`。② **图标 V4 错位呼应**：小星描边减细到 28 保空心清晰，四文件同源。③ 版本升 0.6.9 / code 24 |
| **0.7.0** | ① **iOS 长时间后台回前台整体卡顿根治**：`didBecomeActive` 调 JS 延后 350ms 避开 WebKit `setAllowsActivities(false)` 拒绝窗口；补 `webViewWebContentProcessDidTerminate` 自动重载（60s 冷却，重载走 `bootCheck` 判定不重复签到）；`.appbar`/`.bar` 毛玻璃 `blur(18px)→blur(10px)` 加 `@supports` 降级、删 `.card` 的 `will-change`。② **图标产物规范化**：去掉 `rx` 预烘焙圆角、转 RGB 无 alpha、`tools/gen_icon.py` 产出完整 13 尺寸 appiconset（18 条 Contents.json），`build-ipa.sh` 不再 `sips` 现缩；**左上角 logo 改为与 App 图标同源的空心双星**（原来实心，所以不跟随更新）。③ 新增 `perf_test.go` 5 项回归锁。④ 版本升 0.7.0 / code 25 |
| **0.7.1** | ① **推翻 0.7.0 的卡顿判断**：350ms 延后无效，用户实测锁屏 4 分钟回来「点哪都没反应」。真因是 WebContent 进程被**冻结**后不恢复（WebKit Bug 321511：suspended and idle、`webContentProcessDidTerminate` 不触发），客户端无法唤醒，只能重建。改为「探活 + 两级重建」：离开 ≥1.5s 发 `1+1` 探针，700ms 无响应即判定冻结 → 先 `load()`，1.2s 未完成 → 改 `.id()` 销毁重建整个 WKWebView；重建期间盖同色遮罩。② **补 `WKURLSchemeTask` 失效保护**（`liveTasks` 集合）：重建必然 cancel in-flight 请求，Go 侧跑完仍会回调，旧代码注释写了「忽略」但没检查，会抛异常崩溃。③ **图标描边 48/28 → 30/17**（白色占比 8.03%→5.23%，对齐参考实现 3.96%），logo 同步 1.10/0.62。④ 版本升 0.7.1 / code 26 |
| **0.7.2** | ① **卡顿：0.7.1 的探活方案也失败，改为「不判断，直接换实例」**。探活判据错在「能执行 JS ≠ 能响应触摸」——卡死时常见脚本活着、触摸全吞，探针返回 2 就判定健康，反而把自己锁死；恢复链路跨两个 runloop 三个 `asyncAfter`，任一环没排上就断，`recovering` 还停在 true 把后续事件全挡掉。现改为三档：<1.5s 只刷新 / 1.5~10s 探活（不打断签到）/ **≥10s 直接 `hardReset()`**；`hardReset` 一个 runloop 内断回调 → 置 nil → 换 `webGen`；3s 未 `didFinish` 显示**原生重试页**。② **`PanelSchemeHandler` 加并发闸门（8）+ 请求超时（普通 20s / 签到 180s，超时 504）**，`OnceFlag` 保证只结算一次——后台积压请求回前台会把 GCD 线程占满，表现同样是「点哪都没反应」。③ **图标模糊真因**：`CFBundleIconFiles=["AppIcon60","AppIcon120","AppIcon180"]` 而 iOS 只认「基础名+@2x/@3x」，@3x 屏要 180px 却只找到 `AppIcon60.png`(60px) 放大 3 倍；且从未写 `CFBundleIconName`，`Assets.car` 一次没用过。修为 `CFBundleIconName=AppIcon` + 平铺按 `AppIcon60@2x/@3x` 正确命名 + `CFBundleIconFiles` 只列基础名。④ release 关 `isInspectable`、`isOpaque=true`、`StatusBarStyle.dark` 同值短路。⑤ 新增 `icon_plist_test.go` 4 项回归锁。⑥ 版本升 0.7.2 / code 27 |
