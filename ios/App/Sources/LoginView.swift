import SwiftUI
import WebKit

/// 登录容器：WKWebView 打开平台官网，用户自己完成登录。
///
/// 【重要】不做任何「自动判定登录成功」的收尾。
/// 之前的实现在 `didFinish` 里看 URL + cookie 长度就自动关闭，而官网首页
/// 本身就带一堆埋点 cookie，结果刚打开就被判定为「已登录」直接回跳，
/// 用户根本来不及找登录入口。改成：只有用户点「完成」才提取并关闭。
///
/// 同理，「完成」绝不能靠改 @State 重建本视图来触发——重建会新建一个
/// WKWebView（nonPersistent store 是内存态），之前登录攒下的 cookie 全丢，
/// 提取到空串，表现为「点了完成也没加上账号」。改用通知让同一个
/// Coordinator 在现有 webView 上取 cookie。
struct LoginView: UIViewControllerRepresentable {
    /// 「完成」按钮发这个通知，Coordinator 收到后提取 cookie
    static let extractNotification = Notification.Name("co.repo.points.extractCookies")
    /// 没读到任何 cookie 时发这个，登录页顶部会给出提示而不关闭
    static let extractFailedNotification = Notification.Name("co.repo.points.extractCookiesFailed")

    let provider: String
    let url: URL
    /// 会话 cookie 可能所在的父域（百度系 / 用户中心系平台用）。空则只看当前 host。
    var cookieHost: String = ""
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
        return vc
    }

    func updateUIViewController(_ vc: UIViewController, context: Context) {}

    static func dismantleUIViewController(_ vc: UIViewController, coordinator: Coordinator) {
        NotificationCenter.default.removeObserver(coordinator)
    }

    final class Coordinator: NSObject, WKNavigationDelegate {
        var parent: LoginView
        weak var web: WKWebView?
        /// 防止重复回调：结算一次后置位
        private var settled = false

        init(_ parent: LoginView) {
            self.parent = parent
            super.init()
            NotificationCenter.default.addObserver(
                self, selector: #selector(onExtractRequest),
                name: LoginView.extractNotification, object: nil)
        }

        @objc private func onExtractRequest() {
            guard !settled else { return }
            extractNow { [weak self] cookies in
                guard let self else { return }
                guard let cookies, !cookies.isEmpty else {
                    NotificationCenter.default.post(name: LoginView.extractFailedNotification, object: nil)
                    return
                }
                self.finish(cookies)
            }
        }

        /// 统一出口：只回调一次
        func finish(_ cookies: String?) {
            guard !settled else { return }
            settled = true
            DispatchQueue.main.async { self.parent.onFinish(cookies) }
        }

        // 刻意不实现 didFinish 自动收尾。登录是否完成由用户判断，
        // 页面的 URL / cookie 长度都不足以作为凭据。

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

        /// 手动「完成」：只取当前站点 / 配置的父域的 cookie（多站点混拼会串号）
        func extractNow(_ done: @escaping (String?) -> Void) {
            guard let web else { done(nil); return }
            let host = web.url?.host ?? ""
            // 去重，保持优先级：当前站点 → 配置的父域 → 初始地址
            var candidates: [String] = []
            for h in [host, parent.cookieHost, parent.url.host ?? ""] where !h.isEmpty {
                if !candidates.contains(h) { candidates.append(h) }
            }
            web.configuration.websiteDataStore.httpCookieStore.getAllCookies { cookies in
                var joined = ""
                for h in candidates {
                    joined = LoginView.siteCookies(cookies, host: h)
                    if !joined.isEmpty { break }
                }
                DispatchQueue.main.async { done(joined.isEmpty ? nil : joined) }
            }
        }
    }

    /// 只保留与给定 host 相关的 cookie，按 `k=v; k2=v2` 拼接。
    ///
    /// 双向匹配：host 是 cookie 域的子域（当前页面）、或 cookie 域是 host 的子域
    /// （传父域 baidu.com 时把 .wenxin.baidu.com 的 cookie 也带上）。
    ///
    /// 不过滤 `__Host-` / `__Secure-` 前缀：那是浏览器对「设置 cookie」的约束，
    /// 作为请求头原样发回服务端完全有效，砍掉反而会丢掉真正的会话项。
    static func siteCookies(_ cookies: [HTTPCookie], host: String) -> String {
        guard !host.isEmpty else { return "" }
        return cookies
            .filter { ck in
                let d = ck.domain.hasPrefix(".") ? String(ck.domain.dropFirst()) : ck.domain
                return host == d || host.hasSuffix("." + d) || d.hasSuffix("." + host)
            }
            .map { "\($0.name)=\($0.value)" }
            .joined(separator: "; ")
    }
}
