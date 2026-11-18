import SwiftUI
import WebKit

/// 登录容器：WKWebView 打开平台官网，用户自己完成登录。
///
/// 与 Android 的 LoginActivity 同构。区别只在 cookie 提取方式：
/// iOS 上 `WKWebsiteDataStore.httpCookieStore` 能拿到含 HttpOnly 的全部 cookie。
struct LoginView: UIViewControllerRepresentable {
    let provider: String
    let url: URL
    /// true 时忽略自动判定，直接把当前站点 cookie 交出去（点「完成」的路径）
    var forceExtract: Bool = false
    var extractTick: Int = 0
    var onFinish: (String?) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIViewController(context: Context) -> UIViewController {
        let cfg = WKWebViewConfiguration()
        // 每个账号一个独立 store：彻底杜绝串号
        cfg.websiteDataStore = .nonPersistent()
        let web = WKWebView(frame: .zero, configuration: cfg)
        web.navigationDelegate = context.coordinator

        // 必须给容器一个非零尺寸：之前的实现用的是零尺寸 UIViewController，
        // WKWebView 在这种父视图里可能不加载页面，表现是「登录页一片空白」。
        let vc = UIViewController()
        vc.view.backgroundColor = .systemBackground
        vc.preferredContentSize = CGSize(width: 390, height: 844)
        vc.view.addSubview(web)
        web.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            web.topAnchor.constraint(equalTo: vc.view.safeAreaLayoutGuide.topAnchor),
            web.bottomAnchor.constraint(equalTo: vc.view.bottomAnchor),
            web.leadingAnchor.constraint(equalTo: vc.view.leadingAnchor),
            web.trailingAnchor.constraint(equalTo: vc.view.trailingAnchor),
        ])

        web.load(URLRequest(url: url))
        context.coordinator.web = web
        context.coordinator.parent = self

        // 点「完成」会以 forceExtract=true 重建一次，这里等页面状态稳定后直接提取。
        if forceExtract {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) { [weak self] in
                context.coordinator.extractNow { self?.onFinish($0) }
            }
        }
        return vc
    }

    func updateUIViewController(_ vc: UIViewController, context: Context) {}
            guard let host = context.coordinator.web?.url?.host else {
                DispatchQueue.main.async { self.onFinish(nil) }
                return
            }
            let scoped = cookies.filter { ck in
                let d = ck.domain.hasPrefix(".") ? String(ck.domain.dropFirst()) : ck.domain
                return !host.isEmpty && (host == d || host.hasSuffix(d))
            }
            let joined = scoped
                .filter { !$0.name.hasPrefix("__") }
                .map { "\($0.name)=\($0.value)" }
                .joined(separator: "; ")
            DispatchQueue.main.async { self.onFinish(joined.isEmpty ? nil : joined) }
        }
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var parent: LoginView
        weak var web: WKWebView?

        init(_ parent: LoginView) {
            self.parent = parent
        }

        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            // 登录成功后自动收尾。判定必须同时满足：
            //   1) 停留在该平台自己的域（不是 accounts./passport 之类的登录域）；
            //   2) 已经离开登录页路径；
            //   3) cookie 里确实有会话项。
            // 只看 cookie 长度会误判——登录页本身也可能带一长串 cookie。
            guard let current = webView.url,
                  let host = current.host,
                  !host.contains("passport"),
                  !host.hasPrefix("accounts."),
                  !current.path.contains("/login"),
                  !current.path.contains("/signin") else { return }

            webView.configuration.websiteDataStore.httpCookieStore.getAllCookies { cookies in
                let scoped = cookies.filter { ck in
                    // 只取与当前站点相关的 cookie，避免把别的站点的会话混进来
                    host.hasSuffix(ck.domain.hasPrefix(".") ? String(ck.domain.dropFirst()) : ck.domain)
                }
                let joined = scoped
                    .filter { !$0.name.hasPrefix("__") }
                    .map { "\($0.name)=\($0.value)" }
                    .joined(separator: "; ")
                if joined.count > 60 {
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
    /// 手动「完成」：只取当前站点的 cookie（多站点混拼会串号）。
    func extractNow(_ done: @escaping (String?) -> Void) {
        guard let store = web?.configuration.websiteDataStore else { done(nil); return }
        let host = web?.url?.host ?? ""
        store.httpCookieStore.getAllCookies { cookies in
            let scoped = cookies.filter { ck in
                let d = ck.domain.hasPrefix(".") ? String(ck.domain.dropFirst()) : ck.domain
                return !host.isEmpty && (host == d || host.hasSuffix(d))
            }
            let joined = scoped.map { "\($0.name)=\($0.value)" }.joined(separator: "; ")
            DispatchQueue.main.async { done(joined.isEmpty ? nil : joined) }
        }
    }
}
