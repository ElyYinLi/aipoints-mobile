import Foundation
import SwiftUI
import Mobile
import Security

/// Go 引擎的 Swift 门面。
///
/// `Mobile` 这个模块来自 `gomobile bind -target=ios`（产物 Mobile.xcframework）。
/// 与 Android 侧同样的思路：引擎接口一律 JSON 字符串进出，Swift 只做展示与
/// 凭据保管（Keychain）。
@MainActor
final class Engine: ObservableObject {
    static let shared = Engine()

    /// Go 侧面板固定端口。iOS 上同进程内的回环访问不需要任何网络权限声明。
    static let panelAddr = "127.0.0.1:7879"

    @Published private(set) var ready = false
    @Published private(set) var status = "正在启动引擎…"

    var panelURL: URL? { ready ? URL(string: "http://\(Self.panelAddr)/") : nil }

    private var started = false

    func start() {
        guard !started else { return }
        started = true

        let dir = FileManager.default
            .urls(for: .documentDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("engine", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)

        let reply = MobileStart(dir.path) ?? ""
        ready = reply.contains("\"ok\":true")
        status = ready ? "" : "引擎启动失败：\(reply)"
        if ready { restoreAccounts() }
    }

    /// 把 Keychain 里的凭据重新灌回引擎。
    ///
    /// 为什么必须先做这一步：引擎的账号库在 app 沙箱的 engine 目录里，
    /// 重装/系统清理 WebView 数据后可能丢，而真正敏感的部分（cookie）在 Keychain。
    /// 少了这一步，表面上账号还在，一签到就报「缺少 cookie」。
    /// 幂等：已存在同 account_id 的记录会被覆盖为同一份明文。
    private func restoreAccounts() {
        let raw = MobileListAccounts() ?? "[]"
        guard let data = raw.data(using: .utf8),
              let list = try? JSONSerialization.jsonObject(with: data) as? [[String: Any]] else { return }
        for item in list {
            guard let id = item["account_id"] as? String else { continue }
            guard let secret = Keychain.load(key: "secret:" + id) else { continue }
            _ = MobileAddAccount(secret)
        }
    }

    // MARK: - 账号与查询

    func providersJSON() -> String { MobileProviders() ?? "[]" }

    func accountsJSON() -> String { MobileListAccounts() ?? "[]" }

    func summaryJSON() -> String { MobileSummary() ?? "{}" }

    /// 写入账号：secret 先落 Keychain，只把脱敏 JSON 交给引擎。
    func addAccount(provider: String, cookie: String, label: String) -> Bool {
        let accountID = provider + "-" + String(UInt64(bitPattern: Int64(cookie.hashValue)), radix: 16)
        let payload: [String: String] = [
            "provider": provider,
            "account_id": accountID,
            "label": label,
            "cookie": cookie,
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: payload),
              let json = String(data: data, encoding: .utf8) else { return false }

        Keychain.save(key: "secret:" + accountID, value: json)
        let r = MobileAddAccount(json) ?? ""
        return r.contains("\"ok\":true")
    }

    func checkin(provider: String, account: String) -> String {
        MobileCheckinOne(provider, account) ?? "{}"
    }

    func checkinAll() -> String { MobileCheckinAll() ?? "[]" }
}

/// Keychain 封装：凭据不落 UserDefaults、不落文件。
enum Keychain {
    private static let service = "co.repo.points.credentials"

    static func save(key: String, value: String) {
        guard let data = value.data(using: .utf8) else { return }
        let q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
        SecItemDelete(q as CFDictionary)
        var add = q
        add[kSecValueData as String] = data
        // 只有设备解锁后才可读，且不参与 iCloud 备份
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        SecItemAdd(add as CFDictionary, nil)
    }

    static func load(key: String) -> String? {
        let q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess,
              let d = out as? Data else { return nil }
        return String(data: d, encoding: .utf8)
    }

    static func delete(key: String) {
        let q: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
        SecItemDelete(q as CFDictionary)
    }
}
