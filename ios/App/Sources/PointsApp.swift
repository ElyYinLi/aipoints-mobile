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
    /// 进入后台的时刻。用来区分「快速切走又回来」和「真的在后台待了很久」。
    @State private var awayAt: Date?
    /// 正在重建页面时盖一层遮罩，避免看到白闪，也顺手挡住误触。
    @State private var rebuilding = false
    /// 换掉这个值会让 SwiftUI 销毁旧 WKWebView 并新建一个。
    ///
    /// 为什么必须是「新建实例」而不是 `reload()`：出问题的进程已经不再处理任何
    /// 消息，`load()` 发下去也没人接。只有新建 WKWebView 才会向 WebKit 申请
    /// 一个新的 WebContent 进程，这是唯一能绕开坏进程的手段。
    @State private var webGen = UUID()
    /// 重建后 3s 仍没加载完 → 判定这次也失败了，显示原生重试页。
    /// 有它至少用户不用「杀进程重开」，点一下就能再来一次。
    @State private var stalled = false

    var body: some View {
        ZStack {
            PanelWebView(url: engine.panelURL,
                         onReady: { webView = $0 },
                         onLoadFinish: {
                             // 首屏或重建加载完成：收起遮罩。
                             if rebuilding || stalled {
                                 withAnimation(.easeOut(duration: 0.18)) {
                                     rebuilding = false
                                     stalled = false
                                 }
                             }
                         },
                         onRecoverStart: {
                             // 进程被终止、准备重载：先盖上，别让用户看白屏。
                             rebuilding = true
                         })
                // 换 gen 就整体换一个新的 WKWebView（连同新的 WebContent 进程）。
                .id(webGen)
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
            if rebuilding {
                ZStack {
                    (style.dark
                        ? Color(red: 0x14/255.0, green: 0x12/255.0, blue: 0x18/255.0)
                        : Color(red: 0xFB/255.0, green: 0xF8/255.0, blue: 0xFF/255.0))
                    ProgressView().controlSize(.regular)
                }
                .ignoresSafeArea()
                .transition(.opacity)
            }
            // 兜底页：重建 3s 还没起来，说明连新进程都没能拉到页面。
            // 与其让用户对着白屏，不如给一个明确的操作入口。
            if stalled {
                ZStack {
                    (style.dark
                        ? Color(red: 0x14/255.0, green: 0x12/255.0, blue: 0x18/255.0)
                        : Color(red: 0xFB/255.0, green: 0xF8/255.0, blue: 0xFF/255.0))
                    VStack(spacing: 14) {
                        Text("页面没有响应")
                            .font(.headline)
                        Text("重开一次通常就能恢复，账号和签到记录不受影响。")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                        Button("重新加载") { hardReset() }
                            .buttonStyle(.borderedProminent)
                    }
                    .padding(28)
                }
                .ignoresSafeArea()
                .transition(.opacity)
            }
        }
        .onAppear { engine.start() }
        // 回前台的处理（0.7.2 重做）。
        //
        // 演进史，别再走回头路：
        //   0.6.4  把这里的六段触发全删了（用户要求），包括「探活 + 重建」。
        //          当时删是因为它是冲着「读取失败」去的，方向错了。
        //   0.7.0  加了 350ms 延后调 JS，想避开 WebKit 的 setAllowsActivities(false)。
        //          实测无效——用户锁屏 4 分钟回来「点哪都没反应」。
        //   0.7.1  改成「探活（1+1）+ 两级重建」。实测**仍然无效**——
        //          用户反馈「卡顿恢复不了，一直卡」。
        //          复盘：探活本身就要走一次 JS 往返，而 App 卡住时那次往返
        //          可能既不回调也不报错（只有 700ms 定时器兜底，但主线程一旦
        //          被占住，`asyncAfter` 同样排不进去）；而且「先 load、再换
        //          id」的两级结构把一次恢复拆成两个 runloop，任何一环卡住
        //          整条链就断了。
        //   0.7.2  认清：不要再试图「判断进程死没死」。能判断的前提是系统还
        //          愿意回话，而卡死时恰恰什么都不回。唯一确定有效的动作是
        //          「离开足够久就直接换一个新 WKWebView」——不问原因，不给
        //          探针，一个 runloop 内完成拆除 + 换实例。
        //
        // 现在的三档：
        //   离开 < 1.5s   → 页面肯定活着，轻刷一下（省掉重建抖动）
        //   离开 < 10s    → 签到可能正在跑，探一次活；活着就只刷新，别打断
        //   离开 >= 10s   → 直接换实例。签到早就该结束了，卡死就一定是这一档
        .onReceive(NotificationCenter.default.publisher(
            for: UIApplication.didEnterBackgroundNotification)) { _ in
            awayAt = Date()
        }
        .onReceive(NotificationCenter.default.publisher(
            for: UIApplication.didBecomeActiveNotification)) { _ in
            let away = awayAt.map { Date().timeIntervalSince($0) } ?? .greatestFiniteMagnitude
            awayAt = nil
            // 正在重建就别再插一脚：重建还没走完又发探针，只会叠出第二轮 rebuild。
            if rebuilding { return }
            guard let web = webView else { return }

            if away < 1.5 {
                refresh(web)
            } else if away < 10 {
                // 短时间离开：签到可能正在进行，先探活，别粗暴打断。
                probeThenMaybeReset(web)
            } else {
                hardReset()
            }
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

    // MARK: - 回前台恢复

    /// 让页面自己重拉一次数据。页面里没有这个钩子时静默跳过。
    private func refresh(_ web: WKWebView) {
        web.evaluateJavaScript(
            "window.__pointsRefresh && window.__pointsRefresh()",
            completionHandler: nil)
    }

    /// 10s 以内的短暂离开：探一次活。
    ///
    /// 这一档存在的唯一理由是「别打断正在跑的签到」。所以它只处理最坏情况：
    /// 探针 800ms 不回来，就认为页面已经不在处理事件了，升级到 hardReset。
    private func probeThenMaybeReset(_ web: WKWebView) {
        var settled = false
        func settle(alive: Bool) {
            guard !settled else { return }
            settled = true
            if alive { refresh(web) } else { hardReset() }
        }
        web.evaluateJavaScript("1+1") { result, error in
            let ok = (error == nil) && ((result as? NSNumber) != nil)
            settle(alive: ok)
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.8) { settle(alive: false) }
    }

    /// 换掉整个 WKWebView。
    ///
    /// 顺序很重要：
    ///   1. 先断开旧实例的一切回调（navigationDelegate 置 nil），否则它之后
    ///      任何异步回调都可能改到已经被替换掉的 @State，叠出第二轮重建；
    ///   2. `webView = nil` 立刻放掉强引用，让旧实例和它的进程可以被回收；
    ///   3. 换 `webGen`，SwiftUI 在**同一个 runloop** 里创建新实例。
    ///
    /// 不做 `load()`：旧实例已经不处理消息，load 也是白发。
    private func hardReset() {
        guard !rebuilding else { return }
        rebuilding = true
        stalled = false

        if let old = webView {
            old.stopLoading()
            old.navigationDelegate = nil
        }
        webView = nil
        webGen = UUID()

        // 新建的 WebView 从 makeUIView → load → didFinish 全在本地，
        // 正常几十毫秒。3s 还起不来就说明这条链路也断了，交给重试页。
        DispatchQueue.main.asyncAfter(deadline: .now() + 3.0) {
            guard rebuilding else { return }
            withAnimation(.easeOut(duration: 0.2)) {
                rebuilding = false
                stalled = true
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
    ///
    /// 0.7.2：赋值前先比一次。`@Published` 对相同值也会发 objectWillChange，
    /// 而页面每次 render 都可能 postMessage 一次 setTheme——那会让根视图
    /// 反复重算、preferredColorScheme 反复下发，长列表下体感就是「点一下卡半天」。
    var dark: Bool = false {
        willSet {
            guard newValue != dark else { return }
            objectWillChange.send()
        }
    }
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
    /// 页面加载完成（首屏 or 重建后）。用来收起重建遮罩。
    var onLoadFinish: (() -> Void)? = nil
    /// 即将因进程终止而重载，用来盖上遮罩（否则会看到一段白屏）。
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
        // 0.7.2：改回不透明。页面本来就有整屏底色，透明会让 WebView 每帧都要
        // 和父视图重新合成——叠上 appbar 的 backdrop-filter，是「滑一下要喘口气」
        // 的常见来源。底色由 setTheme 同步，切主题时不会露白边。
        web.isOpaque = true
        web.backgroundColor = StatusBarStyle.shared.dark
            ? UIColor(red: 0x14/255.0, green: 0x12/255.0, blue: 0x18/255.0, alpha: 1)
            : UIColor(red: 0xFB/255.0, green: 0xF8/255.0, blue: 0xFF/255.0, alpha: 1)
        #if DEBUG
        // 只在 Debug 开。release 里开着会让 WebKit 常驻保留调试元信息，
        // 内存和合成开销都白付。
        if #available(iOS 16.4, *) { web.isInspectable = true }
        #endif
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
        /// 页面加载完成回调（收起重建遮罩用）。
        var onLoadFinish: (() -> Void)?
        /// 即将重载（盖上遮罩用）。
        var onRecoverStart: (() -> Void)?
        /// 0.6.9：面板 scheme 拦截层。持有它，保证生命周期与 WebView 一致。
        let schemeHandler = PanelSchemeHandler()
        /// 上次因内容进程终止而重建页面的时刻，用于冷却。
        private var lastRecoverAt: Date?

        /// WebContent 进程被系统回收后自动恢复。
        ///
        /// 没这个回调时，进程被系统 kill 掉后页面就是死的：点什么都不响应，
        /// 也不会自动重载。而另一种更隐蔽的情况——进程被**冻结**（suspended）
        /// 而非 kill——不会触发本回调（WebKit Bug 321511），只能靠回前台
        /// 主动换实例（见 ContentView.hardReset）。两条路互补。
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

        /// 页面加载完成 → 通知 SwiftUI 收起重建遮罩。
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
