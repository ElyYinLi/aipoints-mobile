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

    var body: some View {
        ZStack {
            PanelWebView(url: engine.panelURL)
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

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIView(context: Context) -> WKWebView {
        let cfg = WKWebViewConfiguration()
        cfg.websiteDataStore = .default()
        cfg.allowsInlineMediaPlayback = true

        let web = WKWebView(frame: .zero, configuration: cfg)
        web.navigationDelegate = context.coordinator
        web.scrollView.contentInsetAdjustmentBehavior = .never
        web.isOpaque = false
        web.backgroundColor = .clear
        if #available(iOS 16.4, *) { web.isInspectable = true }
        return web
    }

    func updateUIView(_ web: WKWebView, context: Context) {
        guard let url, web.url == nil else { return }
        web.load(URLRequest(url: url))
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        func webView(_ webView: WKWebView,
                     decidePolicyFor navigationAction: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            guard let u = navigationAction.request.url else {
                decisionHandler(.cancel); return
            }
            // 只允许本机回环，其余一律拒绝（不弹 Safari）
            if u.host == "127.0.0.1" || u.host == "localhost" {
                decisionHandler(.allow)
            } else {
                decisionHandler(.cancel)
            }
        }
    }
}
