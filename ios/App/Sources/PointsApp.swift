import SwiftUI
import WebKit
import BackgroundTasks
import Mobile

/// 主界面：全屏 WKWebView，加载 Go 引擎在本机回环端口起的面板。
///
/// 与 Android 侧完全同构：UI 由 Go 侧的单文件 HTML 提供，双端只负责
/// 「起引擎 + 装一个浏览器 + 一个登录容器」。UI 改动只写一遍。
@main
struct PointsApp: App {
    @StateObject private var engine = Engine.shared
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(engine)
                .preferredColorScheme(nil)   // 跟随系统，HTML 里有 prefers-color-scheme
        }
        .onChange(of: scenePhase) { phase in
            // 进后台时登记一次系统后台刷新机会（iOS 不保证时刻，只作为补签机会）。
            // 放在 scenePhase 里而不是 AppDelegate，省掉一个 UIKit 桥接。
            if phase == .background { BackgroundRefresh.schedule() }
        }
    }
}

struct ContentView: View {
    @EnvironmentObject var engine: Engine
    @ObservedObject private var login = LoginCoordinator.shared
    @State private var webView: WKWebView?

    var body: some View {
        ZStack {
            PanelWebView(url: engine.panelURL, onReady: { webView = $0 })
                .ignoresSafeArea(edges: .bottom)
            if !engine.ready {
                VStack(spacing: 12) {
                    ProgressView()
                    Text(engine.status)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .onAppear { engine.start() }
        // 「打开软件即签到」：每次回到前台，只要还有未签账号就自动签一次。
        // 用户明确不需要后台静默定时，这条就够了，且不触碰任何后台权限。
        .onReceive(NotificationCenter.default.publisher(
            for: UIApplication.didBecomeActiveNotification)) { _ in
            webView?.evaluateJavaScript(
                "window.__pointsAutoCheckin && window.__pointsAutoCheckin()", completionHandler: nil)
        }
        .fullScreenCover(item: Binding(
            get: { login.pendingProvider.map(LoginTarget.init) },
            set: { if $0 == nil { login.dismiss() } }
        )) { target in
            LoginSheet(provider: target.provider) { cookies in
                login.dismiss()
                if let cookies { engine.addAccount(provider: target.provider, cookie: cookies, label: target.provider) }
                webView?.reload()
            }
        }
    }
}

/// fullScreenCover 需要一个 Identifiable 的载体
struct LoginTarget: Identifiable {
    let provider: String
    var id: String { provider }
}

/// 登录容器包装：底部「完成」与自动识别都可用
struct LoginSheet: View {
    let provider: String
    var onDone: (String?) -> Void
    /// 点「完成」时强制提取一次：以新参数重建 LoginView 触发提取
    @State private var forcedExtract = false

    var body: some View {
        NavigationView {
            Group {
                if let url = LoginCoordinator.shared.url(for: provider) {
                    LoginView(provider: provider, url: url,
                              forceExtract: forcedExtract,
                              cookieHost: LoginCoordinator.shared.cookieHost(for: provider) ?? "") { cookies in
                        onDone(cookies)
                    }
                    .id(forcedExtract)   // 翻转即重建，makeUIViewController 里走强制提取分支
                } else {
                    Text("未配置登录入口")
                }
            }
            .navigationTitle("已登录？点完成")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { onDone(nil) }
                }
                // 自动收尾有判定条件，个别平台（跳转多、域名特殊）可能识别不到，
                // 给用户一个明确出口，否则只能取消重来。
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") {
                        forcedExtract = true   // 重建 LoginView，用新参数触发强制提取
                    }
                }
            }
        }
    }
}

/// WKWebView 的 SwiftUI 包装。
///
/// 关键配置：
///   - `limitsNavigationsToAppBoundDomains = false`：允许访问 127.0.0.1（回环不受限，
///     但显式写出意图更清楚）；
///   - 关掉 `isInspectable` 之外的调试开关，保持行为可预期；
///   - 禁掉外部导航，避免面板里的误点把用户带到 Safari。
struct PanelWebView: UIViewRepresentable {
    let url: URL?
    /// 把创建好的 webView 回传给 ContentView，供自动签到与新标签页使用
    var onReady: ((WKWebView) -> Void)? = nil

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIView(context: Context) -> WKWebView {
        let cfg = WKWebViewConfiguration()
        cfg.websiteDataStore = .default()
        cfg.allowsInlineMediaPlayback = true

        // 面板里的「添加账号」通过 PointsBridge.openLogin 拉起原生登录容器，
        // 与 Android 侧的 addJavascriptInterface 同名同语义
        cfg.userContentController.add(context.coordinator, name: "PointsBridge")

        let web = WKWebView(frame: .zero, configuration: cfg)
        web.navigationDelegate = context.coordinator
        web.scrollView.contentInsetAdjustmentBehavior = .never
        web.isOpaque = false
        web.backgroundColor = .clear
        if #available(iOS 16.4, *) { web.isInspectable = true }
        DispatchQueue.main.async { onReady?(web) }
        return web
    }

    func updateUIView(_ web: WKWebView, context: Context) {
        guard let url, web.url == nil else { return }
        web.load(URLRequest(url: url))
        context.coordinator.web = web
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKScriptMessageHandler {
        weak var web: WKWebView?

        /// 处理面板发来的消息。
        /// 约定：body 是 {"action":"openLogin","provider":"trae"}
        func userContentController(_ uc: WKUserContentController,
                                   didReceive message: WKScriptMessage) {
            guard message.name == "PointsBridge",
                  let body = message.body as? [String: Any],
                  let action = body["action"] as? String else { return }
            if action == "openLogin" {
                let provider = (body["provider"] as? String) ?? "workbuddy"
                DispatchQueue.main.async {
                    LoginCoordinator.shared.present(provider: provider)
                }
            }
        }

        func webView(_ webView: WKWebView,
                     decidePolicyFor navigationAction: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            guard let u = navigationAction.request.url else {
                decisionHandler(.cancel); return
            }
            // 本机回环放行；points:// 交给登录协调器；其余一律拒绝（不弹 Safari）
            if u.host == "127.0.0.1" || u.host == "localhost" {
                decisionHandler(.allow)
            } else if u.scheme == "points" {
                let q = URLComponents(url: u, resolvingAgainstBaseURL: false)?
                    .queryItems?.first(where: { $0.name == "provider" })?.value
                LoginCoordinator.shared.present(provider: q ?? "workbuddy")
                decisionHandler(.cancel)
            } else {
                decisionHandler(.cancel)
            }
        }
    }
}

/// 后台刷新：iOS 只保证「系统愿意给的时候」执行一次，不保证时刻。
/// 这里只做一次「打开面板 → 触发一次自动签到」，不常驻、不保活。
///
/// 说明：真实生效需要主 target 打开 Background Modes(fetch)，且
/// BGTaskSchedulerPermittedIdentifiers 里登记该标识（Info.plist 已写）。
/// 拿不到调度时就静默返回——前台「打开即签到」才是主路径。
enum BackgroundRefresh {
    static let taskID = "co.repo.points.refresh"

    static func schedule() {
        guard #available(iOS 13.0, *) else { return }
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: taskID)
        let req = BGAppRefreshTaskRequest(identifier: taskID)
        req.earliestBeginDate = Date(timeIntervalSinceNow: 45 * 60)  // 至少 45 分钟后
        try? BGTaskScheduler.shared.submit(req)
    }

    static func register() {
        guard #available(iOS 13.0, *) else { return }
        BGTaskScheduler.shared.register(forTaskWithIdentifier: taskID, using: nil) { task in
            task.expirationHandler = { task.setTaskCompleted(success: false) }
            // 引擎此刻可能还没起来（冷启动后台唤醒）：先用已有账号库做一次签到
            DispatchQueue.global(qos: .utility).async {
                let reply = MobileCheckinAll() ?? "[]"
                task.setTaskCompleted(success: reply.contains("\"error\":\"") == false)
                schedule()
            }
        }
    }
}
