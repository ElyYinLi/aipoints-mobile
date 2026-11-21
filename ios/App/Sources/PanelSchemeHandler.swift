import Foundation
import WebKit
import Mobile

/// 0.6.9 方案 A2：用自定义 scheme 承载整个面板（主文档 + 所有 API），
/// 彻底绕开「WebView 经本机 TCP server 取数」这条会被 iOS 后台冻结/回收的链路。
///
/// 为什么这样能根治：Apple TN2277 明确说明，App 进入后台被 suspended 后，
/// listening socket 的监听停止，系统还可能回收其资源；即使 App 回到前台，
/// 原 listener 也不再 accept，只有重新 net.Listen 才恢复——这正是用户
/// 「重建页面没用、杀掉 App 重开才行」的官方解释。iOS 还会在锁屏时优化
/// WKWebView「不发网络连接」。既然 HTTP 监听在 iOS 后台不可靠，就干脆不用它：
/// 页面与 API 全部走 WKURLSchemeHandler 在进程内直接分发给 Go（纯内存调用，
/// 零 socket），iOS 的后台挂起机制无从切断。
///
/// 前端零改动：页面从 pointsapi://panel/ 加载后，fetch('/api/xxx') 会自动解析成
/// 同源的 pointsapi://panel/api/xxx，被本 handler 拦截。登录仍走 points://，
/// 与本机 server 一起保留（OAuth 302 需要真实 HTTP），互不影响。

final class PanelSchemeHandler: NSObject, WKURLSchemeHandler {
    static let scheme = "pointsapi"
    static let host = "panel"
    static var baseURL: URL { URL(string: "\(scheme)://\(host)/")! }

    /// 回调用来把「本地 server 是否可用」等信息透给诊断（可选）。
    var onDispatch: ((String) -> Void)?

    /// 0.7.1：活跃 task 集合。
    ///
    /// 这是必须的，不是防御性编程。WKURLSchemeTask 有硬规则：一旦 WebKit 调用了
    /// `webView(_:stop:)`，之后再对它调 `didReceive` / `didFinish` 会抛
    /// NSInternalInconsistencyException **直接崩溃**。
    ///
    /// 而 0.7.1 新增了「回前台检测到进程冻结就重建页面」——重建必然 cancel 掉
    /// 上一轮所有 in-flight 请求（包括那条卡住的 /api/query）。Go 侧的
    /// MobilePanelDispatch 无法取消，它跑完后照样会回到主线程回调；
    /// 这时若不做检查，就是稳定可复现的崩溃。
    ///
    /// 旧代码在这里写了注释「task 已停止——忽略，避免崩溃」，但并没有真的检查。
    private var liveTasks = Set<ObjectIdentifier>()

    private func key(_ task: WKURLSchemeTask) -> ObjectIdentifier {
        ObjectIdentifier(task as AnyObject)
    }

    func webView(_ webView: WKWebView, start urlSchemeTask: WKURLSchemeTask) {
        let req = urlSchemeTask.request
        let url = req.url ?? URL(string: "\(Self.scheme)://\(Self.host)/")!
        let path = url.path.isEmpty ? "/" : url.path
        let method = req.httpMethod ?? "GET"
        let taskKey = key(urlSchemeTask)
        liveTasks.insert(taskKey)

        // 时区偏移：Go 在 gomobile 里读不到系统时区，日志需要页面上报。
        // 自定义 scheme 的 request header 由 fetch 带过来（前端 j() 已统一加 X-TZ-Offset）。
        let tz = req.value(forHTTPHeaderField: "X-TZ-Offset") ?? ""

        // body：优先用 httpBody；万一某些 iOS 版本下自定义 scheme 的 POST body 被
        // 系统 strip，回退读前端 j() 同时写进 header 的 X-Points-Body（已 encodeURIComponent）。
        var body = ""
        if let data = req.httpBody, !data.isEmpty {
            body = String(data: data, encoding: .utf8) ?? ""
        }
        if body.isEmpty, let enc = req.value(forHTTPHeaderField: "X-Points-Body") {
            body = enc.removingPercentEncoding ?? enc
        }

        // query：把 URL 的 query 拼进 target，保留 ?provider=xxx 这类参数。
        var target = path
        if let q = url.query, !q.isEmpty { target += "?" + q }

        // gomobile 导出函数是同步的，但签到等接口可能耗时数秒——必须放到后台队列，
        // 否则会卡住 WKWebView 的调用线程（表现为「点一下界面卡住」）。
        DispatchQueue.global(qos: .userInitiated).async {
            let reply = MobilePanelDispatch(method, target, body, tz) ?? ""
            var status = 200
            var respBody = reply
            if let data = reply.data(using: .utf8),
               let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] {
                if let s = obj["status"] as? Int { status = s }
                if let b = obj["body"] as? String { respBody = b }
            }

            // 主文档是 HTML；其余 /api 都是 JSON。按 path 决定 Content-Type。
            let mime = (path == "/" || path == "/index.html") ? "text/html" : "application/json"
            let headers = ["Content-Type": mime, "Cache-Control": "no-store"]
            let response = HTTPURLResponse(url: url, statusCode: status,
                                           httpVersion: nil, headerFields: headers)!
            let data = respBody.data(using: .utf8) ?? Data()

            // WKURLSchemeTask 的回调必须在主线程。
            DispatchQueue.main.async {
                // 关键：task 可能已被 stop（页面重建、导航离开、进后台被取消）。
                // 已失效就什么都别调，否则崩溃。
                guard self.liveTasks.remove(taskKey) != nil else { return }
                urlSchemeTask.didReceive(response)
                urlSchemeTask.didReceive(data)
                urlSchemeTask.didFinish()
                self.onDispatch?("\(method) \(target) -> \(status)")
            }
        }
    }

    func webView(_ webView: WKWebView, stop urlSchemeTask: WKURLSchemeTask) {
        // Go 侧调用无法取消，但必须标记失效：Go 跑完后回主线程时靠这个集合判断
        // 该不该回调。旧实现这里是空函数，等于放弃了唯一的保护点。
        liveTasks.remove(key(urlSchemeTask))
    }
}
