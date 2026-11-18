import SwiftUI
import WebKit

/// 登录容器：WKWebView 打开平台官网，用户自己完成登录。
///
/// 与 Android 的 LoginActivity 同构。区别只在 cookie 提取方式：
/// iOS 上 `WKWebsiteDataStore.httpCookieStore` 能拿到含 HttpOnly 的全部 cookie。
///
/// 自动收尾的判定（缺一不可）：
///   1) 停留在平台自己的域（排除 passport/accounts/login 等登录域）；
///   2) 已离开登录页路径；
///   3) cookie 里确实有会话项。
/// 只看 cookie 长度会误判——登录页本身也可能带一长串 cookie。
struct LoginView: UIViewControllerRepresentable {
    let provider: String
    let url: URL
    /// true 时跳过自动判定，直接把当前站点 cookie 交出去（「完成」按钮路径）
    var forceExtract: Bool = false
    var onFinish: (String?) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIViewController(context: Context) -> UIViewController {
        let cfg = WKWebViewConfiguration()
        // 每次登录一个独立 store：彻底杜绝串号
        cfg.websiteDataStore = .nonPersistent()
        let web = WKWebView(frame: .zero, configuration: cfg)
        web.navigationDelegate = context.coordinator

        // 容器必须有非零尺寸：零尺寸父视图里的 WKWebView 可能不加载页面（白屏）
        let vc = UIViewController()
        vc.view.backgroundColor = .systemBackground
        vc.view.addSubview(web)
        web.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            web.topAnchor.constraint(equalTo: vc.view.safeAreaLayoutGuide.topAnchor),
            web.bottomAnchor.constraint(equalTo: vc.view.bottomAnchor),
            web.leadingAnchor.constraint(equalTo: vc.view.leadingAnchor),
            web.trailingAnchor.constraint(equalTo: vc.view.trailingAnchor),
        ])

        context.coordinator.web = web
        web.load(URLRequest(url: url))

        // 点「完成」会以 forceExtract=true 重建一次，等页面状态稳定后直接提取
        if forceExtract {
            let coord = context.coordinator
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) {
                coord.extractNow(coord.parent.onFinish)
            }
        }
        return vc
    }

    func updateUIViewController(_ vc: UIViewController, context: Context) {}

    static func dismantleUIViewController(_ vc: UIViewController, coordinator: Coordinator) {
        coordinator.cancelPending()
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var parent: LoginView
        weak var web: WKWebView?
        /// 防止重复回调：结算一次后置位
        private var settled = false
        private var pending: DispatchWorkItem?

        init(_ parent: LoginView) {
            self.parent = parent
        }

        func cancelPending() {
            pending?.cancel()
            pending = nil
        }

        /// 统一出口：只回调一次
        func finish(_ cookies: String?) {
            guard !settled else { return }
            settled = true
            cancelPending()
            DispatchQueue.main.async { self.parent.onFinish(cookies) }
        }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            guard !parent.forceExtract else { return }
            guard let current = webView.url,
                  let host = current.host, !host.isEmpty,
                  !host.contains("passport"),
                  !host.hasPrefix("accounts."),
                  !current.path.contains("/login"),
                  !current.path.contains("/signin") else { return }

            // 单页应用登录成功后常有一次异步跳转，等 1.2s 再取，避免取到空会话
            cancelPending()
            let work = DispatchWorkItem { [weak self, weak webView] in
                guard let self, let store = webView?.configuration.websiteDataStore else {
                    self?.finish(nil); return
                }
                store.httpCookieStore.getAllCookies { cookies in
                    let joined = LoginView.siteCookies(cookies, host: host)
                    // 登录页本身也可能有 cookie，要求有一定体量才算会话建立
                    if joined.count > 60 {
                        self.finish(joined)
                    }
                }
            }
            pending = work
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.2, execute: work)
        }

        func webView(_ webView: WKWebView,
                     decidePolicyFor navigationAction: WKNavigationAction,
                     decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
            if let s = navigationAction.request.url?.scheme,
               s == "http" || s == "https" {
                decisionHandler(.allow)
            } else {
                decisionHandler(.cancel)
            }
        }

        /// 手动「完成」：只取当前站点的 cookie（多站点混拼会串号）
        func extractNow(_ done: @escaping (String?) -> Void) {
            guard let web else { done(nil); return }
            let host = web.url?.host ?? ""
            let fallback = parent.url.host ?? ""
            web.configuration.websiteDataStore.httpCookieStore.getAllCookies { cookies in
                var joined = LoginView.siteCookies(cookies, host: host)
                if joined.isEmpty { joined = LoginView.siteCookies(cookies, host: fallback) }
                DispatchQueue.main.async { done(joined.isEmpty ? nil : joined) }
            }
        }
    }

    /// 只保留与给定 host 相关的 cookie，按 `k=v; k2=v2` 拼接。
    static func siteCookies(_ cookies: [HTTPCookie], host: String) -> String {
        guard !host.isEmpty else { return "" }
        return cookies
            .filter { ck in
                let d = ck.domain.hasPrefix(".") ? String(ck.domain.dropFirst()) : ck.domain
                return host == d || host.hasSuffix("." + d)
            }
            // __Host-/__Secure- 前缀是浏览器侧约束，塞进 HTTP 头无意义还占长度
            .filter { !$0.name.hasPrefix("__") }
            .map { "\($0.name)=\($0.value)" }
            .joined(separator: "; ")
    }
}
