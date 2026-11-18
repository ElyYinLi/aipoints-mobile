import SwiftUI
import WebKit

/// 主界面：全屏 WKWebView，加载 Go 引擎在本机回环端口起的面板。
///
/// 与 Android 侧完全同构：UI 由 Go 侧的单文件 HTML 提供，双端只负责
/// 「起引擎 + 装一个浏览器 + 一个登录容器」。UI 改动只写一遍。
@main
struct PointsApp: App {
    @StateObject private var engine = Engine.shared

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(engine)
                .preferredColorScheme(nil)   // 跟随系统，HTML 里有 prefers-color-scheme
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
    @State private var coordinator: LoginView.Coordinator?

    var body: some View {
        NavigationView {
            Group {
                if let url = LoginCoordinator.shared.url(for: provider) {
                    LoginView(provider: provider, url: url) { cookies in
                        onDone(cookies)
                    }
                } else {
                    Text("未配置登录入口")
                }
            }
            .navigationTitle("登录 \(provider)")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { onDone(nil) }
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
