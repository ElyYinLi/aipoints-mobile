import SwiftUI
import WebKit
import Combine
import BackgroundTasks
import UniformTypeIdentifiers
import Mobile

/// 主界面：全屏 WKWebView，加载 Go 引擎在本机回环端口起的面板。
///
/// 与 Android 侧完全同构：UI 由 Go 侧的单文件 HTML 提供，双端只负责
/// 「起引擎 + 装一个浏览器 + 一个登录容器」。UI 改动只写一遍。
@main
struct PointsApp: App {
    @StateObject private var engine = Engine.shared
    @Environment(\.scenePhase) private var scenePhase
    @ObservedObject private var style = StatusBarStyle.shared

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(engine)
                // 主题联动：面板切到浅/深色时，这里跟着切，系统就会自动把状态栏
                // 的图标（时间/信号/电池）调成深字或白字。之前写死 nil（纯跟随系统），
                // 用户在面板里手动切换主题后状态栏还是系统色，看着就是「颜色不对」。
                .preferredColorScheme(style.dark ? .dark : .light)
        }
        // 0.6.4：删掉了 scenePhase 里的后台刷新登记。
        // 原来每次进后台都排一次 BGAppRefreshTask，到点就在后台跑
        // MobileCheckinAll()——这正是用户说的「动不动就给我去签到」，
        // 而且用户完全看不见它跑过。现在 App 没有任何后台执行机会。
    }
}

struct ContentView: View {
    @EnvironmentObject var engine: Engine
    @ObservedObject private var login = LoginCoordinator.shared
    @State private var webView: WKWebView?
    /// 进入后台的时刻。0.6.4 起只作诊断，不再参与「回来要不要重建」的判断。
    @State private var awayAt: Date?

    var body: some View {
        ZStack {
            PanelWebView(url: engine.panelURL, onReady: { webView = $0 })
                // 全屏铺满：底部让内容延伸到 Home Indicator，顶部也交给页面自己
                // 用 env(safe-area-inset-top) 留白——这样状态栏区域就是页面背景色，
                // 不会出现「系统默认背景 + 内容区」两截色。
                .ignoresSafeArea()
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
        // 0.6.4：回到前台只刷新一次列表，别的一律不做。
        //
        // 这里原本有三段（用户要求「这6个触发全部删掉」「你现有的措施全部删掉」）：
        //   1. 首次 didBecomeActive 注入 __pointsAutoCheckin()  → 删
        //   2. 后台 >45s 就 reloadFromBlank() 整页重建          → 删
        //   3. 注入探活 + DOM 探测（.card,.row 为空即判定页面已死）→ 删
        //
        // 第 1 段会让「一天开几次 App 就签几轮」，是黑号的直接来源；
        // 第 2、3 段是冲着「读取失败」去的，但真因在引擎侧复用了系统已回收的
        // keep-alive 连接（已改成短连接），重建页面只是把界面刷白。
        // 现在读不到就如实显示「读取失败 + 重试」，不重建、不猜。
        .onReceive(NotificationCenter.default.publisher(
            for: UIApplication.didEnterBackgroundNotification)) { _ in
            awayAt = Date()
        }
        .onReceive(NotificationCenter.default.publisher(
            for: UIApplication.didBecomeActiveNotification)) { _ in
            awayAt = nil
            guard let web = webView, web.url != nil else { return }
            web.evaluateJavaScript(
                "window.__pointsRefresh && window.__pointsRefresh()",
                completionHandler: nil)
        }
        .fullScreenCover(item: Binding(
            get: { login.pendingProvider.map(LoginTarget.init) },
            set: { if $0 == nil { login.dismiss() } }
        )) { target in
            LoginSheet(provider: target.provider) { cookies in
                login.dismiss()
                // WorkBuddy 是 OAuth：凭据是引擎换回来的 access token，
                // cookie 不代表登录态，绝不能拿它建一条「只有 cookie」的废账号。
                // 注意：空串表示「完成但没 cookie」，nil 才是「取消」。
                if target.provider != "workbuddy", let cookies, !cookies.isEmpty {
                    engine.addAccount(provider: target.provider, cookie: cookies, label: target.provider)
                }
                if cookies == nil {
                    // 取消：清掉待办标记，避免 reload 后白白轮询 60 秒
                    webView?.evaluateJavaScript(
                        "try{localStorage.removeItem('oauth_pending')}catch(e){}", completionHandler: nil)
                }
                // 登录完成后重载页面，让新账号立刻出现在列表里
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

/// StatusBarStyle：页面主题 → 系统界面明暗。
///
/// 做法说明：SwiftUI 里控制状态栏最稳的方式不是去 swizzle UIHostingController，
/// 而是让根视图的 preferredColorScheme 跟着走——系统据此自动把状态栏图标
/// （时间/信号/电池）刷成深色或白色，导航条、弹出菜单的配色也一并正确。
/// 面板用 postMessage 把当前主题推过来，这里只管收。
final class StatusBarStyle: ObservableObject {
    static let shared = StatusBarStyle()
    /// true = 深色主题（状态栏白字 / 深色界面元素）
    @Published var dark: Bool = false
}

/// 登录容器包装：用户在官方页面登录完，点「完成」才提取 cookie。
///
/// 绝不通过改 @State 重建 LoginView 来触发提取——那会新建 WKWebView，
/// 登录攒下的 cookie 全丢。改为发通知，让同一个 Coordinator 在现有页面取。
struct LoginSheet: View {
    let provider: String
    var onDone: (String?) -> Void
    @State private var hint = "登录完成后点右上角「完成」"

    var body: some View {
        NavigationView {
            Group {
                if let url = LoginCoordinator.shared.url(for: provider) {
                    LoginView(provider: provider, url: url,
                              cookieHost: LoginCoordinator.shared.cookieHost(for: provider) ?? "") { cookies in
                        onDone(cookies)
                    }
                } else {
                    Text("未配置登录入口")
                }
            }
            .navigationTitle(hint)
            .navigationBarTitleDisplayMode(.inline)
            .onReceive(NotificationCenter.default.publisher(
                for: LoginView.extractFailedNotification)) { _ in
                hint = "没读到登录信息，请确认已登录后再点「完成」"
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("取消") { onDone(nil) }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("完成") {
                        NotificationCenter.default.post(
                            name: LoginView.extractNotification, object: nil)
                    }
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
            } else if action == "setTheme" {
                // 页面切主题：同步系统界面明暗（状态栏字色随之变化），
                // 并把 WebView 底色跟上——内容小于一屏时下拉不会露出白边。
                let theme = (body["theme"] as? String) ?? "light"
                let dark = (theme == "dark")
                DispatchQueue.main.async {
                    StatusBarStyle.shared.dark = dark
                    self.web?.backgroundColor = dark
                        ? UIColor(red: 0x14/255.0, green: 0x12/255.0, blue: 0x18/255.0, alpha: 1)
                        : UIColor(red: 0xFB/255.0, green: 0xF8/255.0, blue: 0xFF/255.0, alpha: 1)
                }
            } else if action == "saveFile" {
                let name = (body["name"] as? String) ?? "backup.json"
                let content = (body["content"] as? String) ?? ""
                DispatchQueue.main.async { BackupFiles.save(name: name, content: content) }
            } else if action == "pickFile" {
                DispatchQueue.main.async {
                    BackupFiles.pick { text in
                        // 转成 JSON 字面量再注入，换行/引号不会打断 JS
                        guard let data = try? JSONSerialization.data(withJSONObject: [text]),
                              let arr = String(data: data, encoding: .utf8) else { return }
                        // arr 形如 ["..."]，去掉外层方括号就是合法的字符串字面量
                        let literal = String(arr.dropFirst().dropLast())
                        self.web?.evaluateJavaScript(
                            "window.__pointsOnFilePicked && window.__pointsOnFilePicked(\(literal))",
                            completionHandler: nil)
                    }
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

// 0.6.4：BackgroundRefresh 整块删除。
//
// 它注册了 BGAppRefreshTask，在后台跑 MobileCheckinAll()（整轮签到）。
// 用户的要求很明确：「不要天天动不动就给我去签到，容易黑号」。
// 后台执行正是最不可控的一种——用户睡着了它也在签，出事也查不到。
// 现在 iOS 侧没有任何后台任务，只在「打开 App」时判定一次。

/// 备份文件的存/取。
///
/// 为什么走原生：手机上在文本框里复制一长串 JSON 是最容易出错的一步——
/// 选不全、键盘挡住、粘贴到别处就泄露。用系统「文件」面板保存/选择，
/// 文件真正落到 iCloud Drive 或「我的 iPhone」，换机也能再导回来。
enum BackupFiles {
    /// 存到「文件」App：先写临时文件，再弹 UIDocumentPicker 的导出模式。
    static func save(name: String, content: String) {
        let tmp = FileManager.default.temporaryDirectory.appendingPathComponent(name)
        do {
            try content.write(to: tmp, atomically: true, encoding: .utf8)
        } catch {
            toast("保存失败：\(error.localizedDescription)")
            return
        }
        guard let root = topViewController() else { return }
        let picker = UIDocumentPickerViewController(forExporting: [tmp], asCopy: true)
        picker.shouldShowFileExtensions = true
        root.present(picker, animated: true)
    }

    /// 选一个文件读回文本。
    static func pick(completion: @escaping (String) -> Void) {
        guard let root = topViewController() else { return }
        let picker = UIDocumentPickerViewController(
            forOpeningContentTypes: [.json, .plainText, .data], asCopy: true)
        picker.allowsMultipleSelection = false
        let delegate = PickerDelegate(completion: completion)
        // 用 objc 关联对象保活，否则 delegate 立刻被释放，回调不会来
        objc_setAssociatedObject(picker, &pickerKey, delegate, .OBJC_ASSOCIATION_RETAIN)
        picker.delegate = delegate
        root.present(picker, animated: true)
    }

    private static func topViewController() -> UIViewController? {
        let scene = UIApplication.shared.connectedScenes
            .compactMap { $0 as? UIWindowScene }
            .first { $0.activationState == .foregroundActive }
        var vc = scene?.windows.first { $0.isKeyWindow }?.rootViewController
        while let p = vc?.presentedViewController { vc = p }
        return vc
    }

    private static func toast(_ msg: String) {
        DispatchQueue.main.async {
            guard let vc = topViewController() else { return }
            let a = UIAlertController(title: nil, message: msg, preferredStyle: .alert)
            vc.present(a, animated: true)
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.6) { a.dismiss(animated: true) }
        }
    }
}

private var pickerKey: UInt8 = 0

/// UIDocumentPicker 的 delegate。必须强引用存活到用户选完。
final class PickerDelegate: NSObject, UIDocumentPickerDelegate {
    private let completion: (String) -> Void
    init(completion: @escaping (String) -> Void) { self.completion = completion }

    func documentPicker(_ controller: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
        guard let url = urls.first else { return }
        // asCopy: true 时拿到的是沙盒副本，直接读即可，不需要 startAccessing
        let text = (try? String(contentsOf: url, encoding: .utf8)) ?? ""
        completion(text)
    }

    func documentPickerWasCancelled(_ controller: UIDocumentPickerViewController) {
        completion("")
    }
}

// 0.6.4：reloadFromBlank() 已删除。
// 之前用它「强制换掉被系统回收的 WebContent 进程」，但实测（用户原话）
// 「实质什么都没改变」——真因是 HTTP 连接被复用（引擎侧已改短连接）。
// 页面不再自己重建：读不到就显示「读取失败 + 重试」，由用户决定。
