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
    @ObservedObject private var style = StatusBarStyle.shared
    @State private var webView: WKWebView?
    /// 进入后台的时刻。0.7.1 起重新参与判断：离开太久就不指望进程还活着。
    @State private var awayAt: Date?
    /// 0.7.1：正在重建页面时盖一层遮罩，避免看到白闪，也顺手挡住误触。
    @State private var recovering = false
    /// 0.7.1：二级恢复。改这个值会让 SwiftUI 销毁旧 WKWebView 并新建一个。
    ///
    /// 为什么需要：如果 WebContent 进程池整体卡死，连 `load()` 都发不出去
    /// （页面永远等不到 didFinish），只有新建 WKWebView 才能强制拉一个新进程。
    /// 这是最后一道兜底，触发概率很低，但它是唯一能自救的手段。
    @State private var recoverToken = UUID()

    var body: some View {
        ZStack {
            PanelWebView(url: engine.panelURL,
                         onReady: { webView = $0 },
                         onLoadFinish: {
                             // 首屏或重建加载完成：收起遮罩。
                             if recovering { withAnimation(.easeOut(duration: 0.18)) { recovering = false } }
                         },
                         onRecoverStart: {
                             // 进程被终止、准备重载：先盖上，别让用户看白屏。
                             recovering = true
                         })
                // 0.7.1 二级恢复：token 变了就整体换一个新的 WKWebView。
                .id(recoverToken)
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
            // 重建遮罩：盖住整个屏幕，用页面同款底色，看起来像「刷新了一下」而不是白屏。
            if recovering {
                ZStack {
                    (style.dark
                        ? Color(red: 0x14/255.0, green: 0x12/255.0, blue: 0x18/255.0)
                        : Color(red: 0xFB/255.0, green: 0xF8/255.0, blue: 0xFF/255.0))
                    ProgressView().controlSize(.regular)
                }
                .ignoresSafeArea()
                .transition(.opacity)
            }
        }
        .onAppear { engine.start() }
        // 回前台的处理（0.7.1 重做）。
        //
        // 演进史，别再走回头路：
        //   0.6.4  把这里的六段触发全删了（用户要求），包括「探活 + 重建」。
        //          当时删是因为它是冲着「读取失败」去的，方向错了。
        //   0.7.0  加了 350ms 延后调 JS，想避开 WebKit 的 setAllowsActivities(false)。
        //          实测无效——用户锁屏 4 分钟回来「点哪都没反应」。
        //   0.7.1  认清真相：那不是「活动被拒绝」，而是 WebContent 进程被冻结后
        //          **根本没恢复**。此时页面收不到任何事件，evaluateJavaScript
        //          不回回调，webViewWebContentProcessDidTerminate 也不触发
        //          （WebKit Bug 321511 的现场：进程 suspended and idle、
        //          主线程停在 mach_msg 正常空闲、Time Since Fork 23 小时、
        //          进程既没被 kill 也没有 reload）。客户端无法唤醒这种进程，
        //          唯一出路是重建。
        //
        // 所以现在的做法是「先探活，死了就重建」：
        //   离开 < 1.5s  → 快速切一下，进程肯定活着，直接刷新（不探活，省一次往返）
        //   离开 >= 1.5s → 发一个 1+1 探针，700ms 内没回来就判定冻结 → 重建
        //
        // 为什么探针能判定：进程冻结时 runJavaScriptInFrameInScriptWorld 不会
        // 有结果，completionHandler 永远不执行；而活着的进程几十毫秒就回调。
        .onReceive(NotificationCenter.default.publisher(
            for: UIApplication.didEnterBackgroundNotification)) { _ in
            awayAt = Date()
        }
        .onReceive(NotificationCenter.default.publisher(
            for: UIApplication.didBecomeActiveNotification)) { _ in
            let away = awayAt.map { Date().timeIntervalSince($0) } ?? .greatestFiniteMagnitude
            awayAt = nil
            // 正在重建就别再插一脚：重建还没走完又发探针，只会叠出第二轮 rebuild。
            if recovering { return }
            guard let web = webView, web.url != nil else { return }

            if away < 1.5 {
                web.evaluateJavaScript(
                    "window.__pointsRefresh && window.__pointsRefresh()",
                    completionHandler: nil)
                return
            }
            reviveOrRebuild(web)
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

    // MARK: - 0.7.1 回前台恢复

    /// 探活：活着就刷新，死了就重建。
    ///
    /// 为什么不能只靠 `webViewWebContentProcessDidTerminate`：那个回调只在
    /// WebContent 进程被**终止**时触发。而我们遇到的是被**冻结**（suspended）——
    /// 进程还在、内存还在、`Time Since Fork` 甚至能到 20 多小时，但不再处理任何
    /// 事件（WebKit Bug 321511）。这种情况下回调不会来，只能自己探。
    ///
    /// 探针选 `1+1`：返回值是确定的数字，能区分「真的执行了」和「返回 undefined」。
    /// 700ms 是取舍——活进程几十毫秒就回来，死进程永远不回来；
    /// 等太久用户白等，太短可能误判（首次唤醒慢）。
    private func reviveOrRebuild(_ web: WKWebView) {
        var settled = false

        // 用闭包捕获的方式做「只结算一次」：探针回调和超时定时器谁先到算谁。
        func settle(alive: Bool) {
            guard !settled else { return }
            settled = true
            if alive {
                web.evaluateJavaScript(
                    "window.__pointsRefresh && window.__pointsRefresh()",
                    completionHandler: nil)
            } else {
                rebuild(web)
            }
        }

        web.evaluateJavaScript("1+1") { result, error in
            // 有错、或拿不到数值，都按「进程不响应」处理。
            let ok = (error == nil) && ((result as? NSNumber) != nil)
            settle(alive: ok)
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.7) { settle(alive: false) }
    }

    /// 重建页面。用已知的面板入口而不是 `reload()`：进程冻结后 `web.url` 可能
    /// 已经不可靠（可能为空、可能是 about:blank），显式指定才稳。
    ///
    /// 重建后的行为是安全的：
    ///   - 走的是 `bootCheck()` → `/api/daily` 判定，今天跑过就只刷新，**不重复签到**；
    ///   - 账号数据在 Go 侧落盘 + Keychain，与 WebView 无关，不会丢；
    ///   - 页面状态（滚动位置、展开的面板）会重置，但这是换取「能用」的必要代价。
    private func rebuild(_ web: WKWebView) {
        recovering = true
        web.stopLoading()
        web.load(URLRequest(url: PanelSchemeHandler.baseURL))

        // 一级兜底：1.2s 还没 didFinish，说明连 load 都发不出去（进程池卡死），
        // 升级为销毁重建整个 WKWebView。本地 HTML 正常几十毫秒就加载完，
        // 1.2s 已经非常宽松。
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) {
            guard recovering else { return }
            recoverToken = UUID()
        }
        // 二级兜底：万一新建 WKWebView 也起不来（引擎挂了等），4s 后收掉遮罩，
        // 不能让用户一直看着转圈。此时页面会显示它自己的「读取失败 + 重试」。
        DispatchQueue.main.asyncAfter(deadline: .now() + 4.0) {
            if recovering { withAnimation(.easeOut(duration: 0.2)) { recovering = false } }
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
    /// 0.7.1：页面加载完成（首屏 or 重建后）。用来收起重建遮罩。
    var onLoadFinish: (() -> Void)? = nil
    /// 0.7.1：即将因进程终止而重载，用来盖上遮罩（否则会看到一段白屏）。
    var onRecoverStart: (() -> Void)? = nil

    func makeCoordinator() -> Coordinator {
        let c = Coordinator()
        c.onLoadFinish = onLoadFinish
        c.onRecoverStart = onRecoverStart
        return c
    }

    func makeUIView(context: Context) -> WKWebView {
        let cfg = WKWebViewConfiguration()
        cfg.websiteDataStore = .default()
        cfg.allowsInlineMediaPlayback = true

        // 0.6.9 方案 A2：注册自定义 scheme，面板主文档与所有 API 在进程内分发给 Go，
        // 不经 TCP socket。这样 iOS 后台把本机监听 socket 冻结/回收也影响不到面板。
        cfg.setURLSchemeHandler(context.coordinator.schemeHandler,
                                forURLScheme: PanelSchemeHandler.scheme)

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
        // SwiftUI 会反复调 updateUIView，闭包也要跟着换（捕获的 @State 会变）。
        context.coordinator.onLoadFinish = onLoadFinish
        context.coordinator.onRecoverStart = onRecoverStart
        guard let url, web.url == nil else { return }
        web.load(URLRequest(url: url))
        context.coordinator.web = web
    }

    final class Coordinator: NSObject, WKNavigationDelegate, WKScriptMessageHandler {
        weak var web: WKWebView?
        /// 0.7.1：页面加载完成回调（收起重建遮罩用）。
        var onLoadFinish: (() -> Void)?
        /// 0.7.1：即将重载（盖上遮罩用）。
        var onRecoverStart: (() -> Void)?
        /// 0.6.9：面板 scheme 拦截层。持有它，保证生命周期与 WebView 一致。
        let schemeHandler = PanelSchemeHandler()
        /// 0.7.0（A3）：上次因内容进程终止而重建页面的时刻，用于冷却。
        private var lastRecoverAt: Date?

        /// 0.7.0（A2）：WebContent 进程被系统回收后自动恢复。
        ///
        /// 之前完全没有实现这个回调。iOS 在内存吃紧、或在后台待久了的时候
        /// 会直接 kill 掉 WebContent 进程，此时：
        ///   - 页面上的 JS 全部消失，点任何按钮都毫无反应；
        ///   - 进程是被 kill 的，不一定会回前台自动重载；
        ///   - 这就是用户说的「整体卡顿，点一下要反应半天」里最严重的那一种。
        ///
        /// 注意 WebKit Bug 321511 里记录的另一种情况（进程被**挂起**而非 kill）
        /// 不会触发本回调。0.7.0 以为那种能靠「延后 350ms 调 JS」兜住，
        /// 实测无效；0.7.1 改成回前台主动探活，探不到就重建（见 ContentView.reviveOrRebuild）。
        /// 这两条路互补：被 kill 走这里，被冻结走探活。
        ///
        /// 冷却 60s：避免进程反复崩溃时无限 reload（每次 reload 都会重跑
        /// bootCheck，连着 reload 会造成整页反复闪白，比不恢复还糟）。
        func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
            let now = Date()
            if let last = lastRecoverAt, now.timeIntervalSince(last) < 60 {
                return
            }
            lastRecoverAt = now
            onRecoverStart?()
            // 面板是单页应用，没有路由，重新加载面板地址即可回到原状态。
            // 不 reload() 是因为进程被杀后 webView.url 可能已经不可靠，
            // 直接用已知的面板入口更稳。
            webView.load(URLRequest(url: PanelSchemeHandler.baseURL))
        }

        /// 0.7.1：页面加载完成 → 通知 SwiftUI 收起重建遮罩。
        func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
            onLoadFinish?()
        }

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
            // 0.6.9：pointsapi:// 是面板自己的 scheme，放行（由 PanelSchemeHandler 接管）
            if u.scheme == PanelSchemeHandler.scheme {
                decisionHandler(.allow)
            } else if u.host == "127.0.0.1" || u.host == "localhost" {
                // 本机回环放行（WorkBuddy OAuth 302 回调仍需要真实 HTTP）
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
