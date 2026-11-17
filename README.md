# AI 积分助手 · 移动端（私有自用）

iOS / Android 双端原生的 AI 平台积分聚合与签到工具。**个人自用，不发布任何商店。**

## 为什么是这个架构

核心判断：这个项目 80% 工作量不在 UI，而在「让各平台的登录态和接口在手机上跑通」。
所以采用 **Go 引擎 + 极薄原生 UI 壳**：

```
┌─────────────┐   ┌─────────────┐
│  iOS 壳     │   │ Android 壳  │   原生：WebView / Keychain(Keystore) / 后台调度
│ SwiftUI     │   │ Kotlin      │   只做三件事：登录、存凭据、拉起引擎
└──────┬──────┘   └──────┬──────┘
       │  gomobile bind   │  gomobile bind（或 c-shared）
       └────────┬─────────┘
                ▼
       ┌──────────────────┐
       │   Go 引擎 core/  │   ← 全部业务逻辑在这里，一处实现两端复用
       │ provider/ 平台适配│
       │ store/ 账号存储   │
       │ scheduler/ 排期   │
       │ panel/ 内嵌面板   │  http://127.0.0.1:7879（WebView 加载）
       └──────────────────┘
```

**为什么不是 Flutter / RN**：跨端框架省的是 UI 代码，但这个项目的难点是
WKWebView 隔离 cookie、Keychain、后台任务这些平台原生能力——框架包一层反而更难调。
原生壳 + Go 引擎把所有平台差异压缩到 3 个文件以内。

**为什么引擎用 Go**：`gomobile bind` 支持 iOS/Android 双端导出同一份代码；
标准库自带 HTTP/TLS/JSON，无第三方依赖，包体小；交叉编译方便，CI 上 Linux 就能验证。

## 目录

| 路径 | 内容 |
|---|---|
| `core/` | Go 引擎（可独立测试，Linux 上就能跑全套测试） |
| `core/provider/` | 各平台适配器，新增平台只需实现 `Provider` 接口并注册 |
| `core/panel/` | 内嵌本机 HTTP 面板（单文件 HTML，无 CDN 依赖，离线可用） |
| `core/cmd/pointsctl/` | 命令行外壳，无手机时验证接口链路 |
| `spec/api-map.json` | 七家平台的接口地图（逆向产物，事实依据） |
| `docs/` | 设计决策、平台可行性、构建说明 |
| `android/`、`ios/` | 原生壳（建设中） |

## 平台接入进度

| 平台 | 状态 | 说明 |
|---|---|---|
| WorkBuddy | 已接入 | 官方 OAuth 可直出 token，无需设备指纹 |
| 灵犀（WPS） | 已接入 | 纯 cookie + REST |
| 百度搭子 | 已接入 | 纯 cookie + REST |
| Trae | 待接入 | 需 WebView 拿 httpOnly cookie 换 JWT |
| MiniMax | 待接入 | 签名盐值需真实 token 反推 |
| 豆包 | 待接入 | 只能页面自动化，需 WebView 驱动 + UA 伪装 |
| Qoder | **不可行** | 领取依赖桌面端 `runtime-info.exe` 指纹 |

## 快速开始（无需手机）

```bash
cd core
go test ./...                       # 全部单元测试
go run ./cmd/pointsctl -action providers
go run ./cmd/pointsctl -action serve -dir ./data
# 浏览器打开 http://127.0.0.1:7879/
```

加一个账号（桌面调试用，凭据明文落盘，勿用于生产）：

```bash
go run ./cmd/pointsctl -action add -provider lingxi -cookie "$KDOCS_COOKIE" acc1 "我的账号"
```

## 安全约定

1. **凭据不进日志**：所有 provider 只打印状态码与业务码，绝不打印 token / cookie。
2. **移动端凭据进 Keychain / Keystore**：`store.FileStore` 里的明文 JSON 仅供本机 CLI 调试。
3. **面板只监听 127.0.0.1**，不对外暴露；WebView 通过 `NSAllowsLocalNetworking` 访问。
4. **签到串行**：同设备并发批量请求最容易触发风控，引擎内固定 1.2s 间隔。

## 免责

仅供个人账号自用。各平台用户协议普遍禁止脚本化签到，使用有账号受限风险，请自行判断。
不绕过任何验证码，不代他人操作，不上传任何凭据到第三方服务器。
