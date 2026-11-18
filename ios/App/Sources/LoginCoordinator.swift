import SwiftUI
import WebKit

/// 登录协调器：把「打开某平台登录页」这件事从 WebView 里解耦出来。
///
/// SwiftUI 里从 WebView 的 delegate 里弹一个 fullScreenCover 比较绕，
/// 用单例 + 一个 @Published 的待登录平台最省事，ContentView 监听它即可。
@MainActor
final class LoginCoordinator: ObservableObject {
    static let shared = LoginCoordinator()

    /// 非 nil 时 ContentView 会弹出登录页
    @Published var pendingProvider: String?

    private let urls: [String: String] = [
        // WorkBuddy 是 token 型，网页登录拿不到 token。指向本机 302，
        // 由 Go 引擎现取官方 OAuth authUrl 再跳转，登录完轮询换 token。
        "workbuddy": "http://127.0.0.1:7879/login/workbuddy",
        "lingxi":    "https://www.kdocs.cn/",
        // 百度搭子业务站是 dumate.baidu.com；wenxin.baidu.com 是文心一言，
        // 页面上根本没有搭子的登录入口（用户实测找不到）。
        "dumate": "https://dumate.baidu.com/",
        "trae":   "https://www.trae.cn/",
        "aipy":   "https://www.trustoken.cn/",
    ]

    /// 登录成功后 cookie 所在的站点。与 urls 分开，是因为有些平台的登录
    /// 会跳到主站/用户中心，初始地址取不到会话：
    ///   - 百度搭子：会话 cookie 落在 .baidu.com，登录页域名与业务域名不同；
    ///   - AiPy Pro：用户中心在 trustoken.cn，登录流程常在子域之间跳。
    /// 取不到时回落到 urls 的 host。
    private let cookieHosts: [String: String] = [
        "dumate": "baidu.com",
        "aipy":   "trustoken.cn",
        "lingxi": "kdocs.cn",
    ]

    func cookieHost(for provider: String) -> String? {
        cookieHosts[provider] ?? url(for: provider)?.host
    }

    func present(provider: String) {
        if urls[provider] != nil {
            pendingProvider = provider
        } else if let first = urls.keys.first {
            pendingProvider = first
        }
    }

    func url(for provider: String) -> URL? {
        guard let s = urls[provider] else { return nil }
        return URL(string: s)
    }

    func dismiss() {
        pendingProvider = nil
    }
}
