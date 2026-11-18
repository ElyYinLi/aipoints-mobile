# aipy-mobile

把桌面版「AI 积分助手」的签到能力搬到手机上，**iOS + Android 双端原生**，自用、不发布。

## 这是什么

桌面版是 PyInstaller 打包的 Python 程序（加密结论：**未加壳、未混淆**，详见 `docs/`）。
本仓库不复用它的任何代码，只复用从它身上逆向出来的**接口事实**：每个平台登录后
怎么查余额、怎么签到、需要哪些 header。剩下全部重写。

UI 用一套：Go 侧起一个只监听 `127.0.0.1` 的 HTTP 面板（单文件 HTML），
iOS 用 WKWebView、Android 用 WebView 加载它。改界面只改一份。

## 结构

```
core/                 Go 引擎（唯一有业务逻辑的地方）
  provider/           各平台适配器：workbuddy / aipy / dumate / lingxi / trae
  httpx/              零依赖 HTTP 层
  scheduler/          当日随机错峰排期（FNV-1a 稳定哈希）
  store/              账号存储接口（移动端换成 Keychain/Keystore）
  panel/              本机回环面板（内嵌 HTML）
  mobile/             gomobile bind 导出层（JSON 字符串进出）
  cmd/pointsctl/      命令行验证工具（Linux/macOS 直接跑，不用手机）
android/              极薄 Java 壳：WebView + 登录容器 + 前台服务
ios/                  SwiftUI + WKWebView + Keychain
spec/api-map.json     逆向得到的接口地图（机器可读）
.github/workflows/    Android 用 Linux runner，iOS 用 macOS runner（手动触发）
```

## 本地跑（不需要手机）

```bash
cd core
go test ./...
go run ./cmd/pointsctl -action serve -dir /tmp/pts
# 浏览器打开 http://127.0.0.1:7879
```

## 构建

```bash
bash android/build-apk.sh      # 需要 ANDROID_HOME
bash ios/build-ipa.sh          # 只能在 macOS 上跑
```

或者用 CI：Android 每次 push 自动构建；iOS 手动触发（理由见 workflow 里的配额说明）。


## 在受限沙箱里同步代码

如果 `git push` 反复超时（沙箱网络会拦 git 协议端点，带 token 也一样），别修它，
改走 REST API：

```bash
export GH_TOKEN=github_pat_xxx
python3 tools/gh_sync.py --repo OWNER/NAME --dry-run   # 看差异
python3 tools/gh_sync.py --repo OWNER/NAME             # 真推
```

原理与全部实测坑见 [`docs/SANDBOX-GITHUB.md`](docs/SANDBOX-GITHUB.md)。
发版/上传前务必跑 `python3 tools/check_secrets.py .`（CI 已强制执行）。

## 设计约束（都是踩过坑之后定下来的）

| 约束 | 原因 |
|---|---|
| 凭据只进 Keychain / Keystore，不进文件 | 桌面版明文落盘是它最大的安全缺陷，这里不重复 |
| 登录一律走官方网页（系统 WebView） | 不碰用户密码；平台改 OAuth 流程也不影响我们 |
| 同平台多账号用独立 WebView 数据仓 | 否则 cookie 串号，签到会签到别人账上 |
| 签到串行 + 平台间 1.2s 间隔 + 当日随机错峰 | 批量并发是风控触发的最主要原因 |
| gomobile 产出**静态库**而非 dylib | dylib 会被放进 `Frameworks/`，多数自签工具报 `Invalid mach-o file!` |
| UI 走本地 HTML 而不是原生声明式 | 一套代码双端复用，且不引入 SwiftUI/Compose 两套学习成本 |
| GitHub Free 私有仓库 macOS runner 每月只有 200 分钟 | 2000 分钟 ÷ 10 倍率；iOS 构建必须手动触发 |

## 平台支持现状

| 平台 | 认证 | 状态 |
|---|---|---|
| WorkBuddy | OAuth token | 已实现并测试 |
| 灵犀（kdocs） | cookie | 已实现并测试 |
| 百度搭子 | cookie | 已实现并测试 |
| Trae | JWT（需 WebView 取） | 待接 |
| MiniMax | cookie（盐值未确认） | 待确认 |
| 豆包 | 页面自动化 | 只能 WebView 驱动 |
| Qoder | 桌面端指纹 | 移动端无解，只做展示 |

## 许可证

自用项目，不分发。
