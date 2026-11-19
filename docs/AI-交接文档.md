# AI 交接文档

> 给下一个接手这个项目的 AI（或人）看的。目标：读完这一份就能独立继续开发，不用重新逆向、不用再踩已踩过的坑。
>
> 最后更新：2026-11-19 · 对应版本 **0.5.1 (versionCode 10)**

---

## 0. 三十秒速览

**项目是什么**：一个把多家 AI 平台「每日签到 / 领积分 / 抽奖」合到一起的手机 App，名字叫**积分矩阵**。

**技术形态**：Go 写业务引擎，`gomobile bind` 编成 `.aar/.xcframework`，iOS(Swift) / Android(Java) 只做一层原生壳 + WebView；界面是 Go 里内嵌的**单文件 HTML**，跑在本机 `127.0.0.1:7879` 的 HTTP 服务上。

**为什么这么设计**：界面只写一份两端通用；业务逻辑只写一份 Go；凭据不出本机；平台接口改版时只需改一个 provider 文件。

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
├── android/                   Java 壳 + 零 Gradle 构建脚本 build-apk.sh
├── .github/workflows/         core-ci / build-android / build-ios
├── tools/
│   ├── push-via-api.sh        ★ 沙箱里推代码只能用它（见 5.1）
│   ├── check_secrets.py       提交前扫密钥
│   └── gh_sync.py
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

启用方式：在 provider 文件末尾写 `var _ LotteryProvider = (*Xxx)(nil)`，`registry.All()` 会自动用类型断言推断 `Meta.Lottery = true`，UI 自动出现「抽奖」按钮。**不需要**在别处登记。

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

### 3.14 Go 版本

- `t.Context()` 需要 Go 1.24+；本项目测试里用 `context.Background()` 更稳

---

## 4. 各平台实现要点

| 平台 | ID | 鉴权 | 移动端 | 特有能力 | 源码 |
|---|---|---|---|---|---|
| WorkBuddy | `workbuddy` | access token（官方 OAuth） | ✅ | — | `workbuddy.go` |
| 金山灵犀 | `lingxi` | cookie | ✅ | — | `lingxi.go` |
| 百度搭子 | `dumate` | cookie | ✅ | **抽奖** | `dumate.go` |
| Trae | `trae` | token | ✅ | — | `trae.go` |
| AiPy | `aipy` | cookie | ✅ | **任务领取** | `aipy.go` |

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
GITHUB_TOKEN=<PAT> bash tools/push-via-api.sh
```

脚本会：按当前 `HEAD` 的树逐文件建 blob → 建 tree → 建 commit → 移动 `main` 引用。**它推的是 git 已提交的内容**，所以要先本地 commit。

### 5.2 提交流程

```bash
cd core && gofmt -w . && go vet ./... && go test ./...     # 必须全绿
cd .. && python3 tools/check_secrets.py                     # 扫密钥
git add -A && git commit
GITHUB_TOKEN=<PAT> bash tools/push-via-api.sh
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

### 5.6 版本号要改三处

| 文件 | 字段 |
|---|---|
| `core/mobile/mobile.go` | `var Version = "0.5.1"` |
| `android/app/src/main/AndroidManifest.xml` | `versionCode="10"` / `versionName="0.5.1"` |
| `ios/App/Info.plist` | `CFBundleShortVersionString` / `CFBundleVersion` |

外加 `README.md` 的 version badge。

---

## 6. 界面代码导航（`core/panel/index.go`）

这文件 2100 行，全部 UI 都在这。结构：

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
   checkinAll()        一键签到（签到 → 领任务 → 抽奖，三步串联）
   summaryOf()         一键签到结果压成一句话
   openSheet()         通用底部弹层
   openSettings()      设置面板（外观 / 账号管理 / 备份 / ★ 运行日志）
   openLottery()       抽奖面板
   openTasks()         任务面板
   beginTrack()        长按拖拽排序
   applyView()         卡片/紧凑视图切换
```

### 6.1 图标怎么加

1. `core/panel/icons.go` 里加 SVG，键名注册到表里
2. HTML 里写 `{{ICON:name}}`，Go 渲染时替换
3. 需要 JS 动态用时，`ICON('name')` 直接取

### 6.2 加新平台不用改这个文件

信息全来自 `/api/providers` 的 `Meta`。只要 provider 实现正确，卡片自动出现。

### 6.3 改 UI 后的验证方式

```bash
cd core && go run ./cmd/pointsctl -action serve    # 起面板
# 浏览器开 http://127.0.0.1:7879
```

更严谨的做法（本项目一直在用）：Playwright 打桩 `fetch` 造数据 + 截图看渲染。

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
GITHUB_TOKEN=<PAT> bash tools/push-via-api.sh

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

---

## 12. 版本历史

| 版本 | 要点 |
|---|---|
| 0.4.x | 打通 Android 零 Gradle 构建链、AiPy 一键领任务、iOS 前台恢复 reload |
| 0.4.2 | 一键签到合并领任务+抽奖、主题切换、状态栏沉浸、备份文件导入导出 |
| 0.5.0 | 改名**积分矩阵**、抽奖可视化入口、账号拖拽排序、卡片/紧凑双视图、README 重写、加 `v0.5.0` tag |
| **0.5.1** | **修 410110（UUID）、签到 +N 回执（差值反推）、提示统一带单位、设置页运行日志** |
