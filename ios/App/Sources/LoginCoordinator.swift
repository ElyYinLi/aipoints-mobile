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
        "workbuddy": "https://www.workbuddy.cn/",
        "lingxi":    "https://www.kdocs.cn/",
        "dumate":    "https://wenxin.baidu.com/",
        "trae":      "https://www.trae.cn/",
        "aipy":      "https://www.trustoken.cn/",
    ]

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
