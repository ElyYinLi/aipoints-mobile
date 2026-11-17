import SwiftUI
import WebKit

/// 登录容器：WKWebView 打开平台官网，用户自己完成登录。
///
/// 与 Android 的 LoginActivity 同构。区别只在 cookie 提取方式：
/// iOS 上 `WKWebsiteDataStore.httpCookieStore` 能拿到含 HttpOnly 的全部 cookie。
struct LoginView: UIViewControllerRepresentable {
    let provider: String
    let url: URL
    var onFinish: (String?) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIViewController(context: Context) -> UIViewController {
        let cfg = WKWebViewConfiguration()
        // 每个账号一个独立 store：彻底杜绝串号
        cfg.websiteDataStore = .nonPersistent()
        let web = WKWebView(frame: .zero, configuration: cfg)
        web.navigationDelegate = context.coordinator

        let vc = UIViewController()
        vc.view.addSubview(web)
        web.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            web.topAnchor.constraint(equalTo: vc.view.topAnchor),
            web.bottomAnchor.constraint(equalTo: vc.view.bottomAnchor),
            web.leadingAnchor.constraint(equalTo: vc.view.leadingAnchor),
            web.trailingAnchor.constraint(equalTo: vc.view.trailingAnchor),
        ])

        web.load(URLRequest(url: url))
        context.coordinator.web = web
        return vc
    }

    func updateUIViewController(_ vc: UIViewController, context: Context) {}

    final class Coordinator: NSObject, WKNavigationDelegate {
        private let parent: LoginView
        weak var web: WKWebView?

        init(_ parent: LoginView) {
            self.parent = parent
        }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            // 页面加载完就抓一次 cookie。够长就认为登录已生效，自动收尾。
            webView.configuration.websiteDataStore.httpCookieStore.getAllCookies { cookies in
                let joined = cookies
                    .filter { !$0.name.hasPrefix("__") }
                    .map { "\($0.name)=\($0.value)" }
                    .joined(separator: "; ")
                // 太短说明还在登录页，继续等用户操作；长到一定程度才认为拿到会话
                if joined.count > 200 {
                    DispatchQueue.main.async { self.parent.onFinish(joined) }
                }
            }
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
    }
}

/// 手动「完成」按钮用的调用：把当前 cookie 交出去。
extension LoginView.Coordinator {
    func extractNow(_ done: @escaping (String?) -> Void) {
        guard let store = web?.configuration.websiteDataStore else { done(nil); return }
        store.httpCookieStore.getAllCookies { cookies in
            let joined = cookies.map { "\($0.name)=\($0.value)" }.joined(separator: "; ")
            DispatchQueue.main.async { done(joined.isEmpty ? nil : joined) }
        }
    }
}
