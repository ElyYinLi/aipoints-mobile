<div align="center">

# 积分矩阵 · Pointex

**一次点击，把所有平台的签到、任务奖励、抽奖机会全部做完。**

<p>
  <img alt="version" src="https://img.shields.io/badge/version-0.6.2-4F5BD5">
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

它不依赖任何服务器：引擎跑在手机本地回环地址，凭据只存在系统安全存储里。

**核心取舍：宁可少做一个功能，也不把凭据发到云端。**

## 特性

| | |
|---|---|
| ⚡ **打开即全套** | 冷启动自动跑「签到 → 领任务奖励 → 抽奖」，与手动点「一键签到」走的同一条代码路径 |
| 📊 **状态可见** | 顶部活动条逐个账号显示进度（`百度搭子 正在签到…（2/3）`），完成后变绿、失败变红，几秒自动收起 |
| 🔒 **本地优先** | 引擎监听 `127.0.0.1`，凭据进 iOS Keychain / Android Keystore，不经任何第三方服务器 |
| 🎨 **双端同构** | UI 由 Go 侧单文件 HTML 提供，改界面只改一份，iOS 与 Android 完全一致 |
| 📱 **三种视图** | 完整卡片 / 紧凑列表一键切换；长按任意账号可拖动排序 |
| 🌗 **双态主题** | 浅色 / 深色，状态栏图标明暗同步（「跟随系统」在 WebView 里不可靠，已移除） |
| 💾 **免登录迁移** | 备份导出为真实文件（系统文件面板选位置），换机导入即恢复全部账号与登录态 |
| 🧭 **内置运行日志** | 设置页可直接看每条失败请求的时间、路径、状态码与业务码，一键复制反馈；凭据自动脱敏 |
| 🔑 **API 令牌管理** | AiPy 的调用密钥（`sk-` 开头）可在 App 内新建 / 启停 / 删除，额度、模型限制、IP 白名单都能改；误删正在用的那条会被拦下 |
| 🔔 **签到通知** | 企业微信 / 钉钉 / 飞书 / Bark / 自定义 Webhook 五渠道，一次一键签到只推一条汇总，失败项置顶 |
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
│    panel/      本机回环 HTTP 面板（内嵌单文件 HTML）   │
│    store/      账号存储抽象（壳层换成 Keychain/Keystore）│
│    scheduler/  当日随机错峰（FNV-1a 稳定哈希）         │
│    mobile/     gomobile bind 导出层（JSON 字符串进出） │
└──────────────────────────────────────────────────────┘
```

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
