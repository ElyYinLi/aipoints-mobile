package co.repo.points;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Toast;

import java.io.IOException;
import java.net.HttpURLConnection;
import java.net.URL;

/**
 * 主界面：一个全屏 WebView，加载 Go 引擎内嵌在本机回环端口上的面板。
 *
 * 为什么这么设计：
 *   - UI（列表、按钮、配色、暗色模式）全在 Go 侧的单文件 HTML 里，iOS 和 Android
 *     用同一份，改 UI 不用改两次，也不用学两套声明式框架；
 *   - WebView 只连 127.0.0.1，不加载任何外部资源，离线也能开；
 *   - 引擎端口由 Go 侧固定为 127.0.0.1:7879（loopback，不对外暴露）。
 *
 * 启动顺序：Start() 拉起引擎 → 轮询端口就绪 → loadUrl。Go 的 HTTP Server
 * 起得很快，但为了稳，加一个最多 3 秒的等待。
 */
public final class MainActivity extends Activity {

    private static final String URL_BASE = "http://127.0.0.1:7879";
    private static final int LOGIN_REQ = 1001;
    private static final int SAVE_REQ = 1002;
    private static final int PICK_REQ = 1003;

    private WebView web;
    private final Handler ui = new Handler(Looper.getMainLooper());
    /**
     * 进入后台的时刻（毫秒）。
     *
     * 0.6.4：只作诊断用，不再参与「回来要不要重建」的判断——
     * 那条阈值（>45s rebuild）已按用户要求删除。
     */
    private long awayAt = 0;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);

        // 全面屏（edge-to-edge）：
        //   - 之前用 Theme.DeviceDefault.Light.NoActionBar + 默认状态栏，系统给状态栏刷的是
        //     一条固定的白色 + 深色图标带，和页面背景对不上，看着像「上面贴了一条」。
        //   - 现在把系统栏透明、让 WebView 铺满全屏，状态栏图标颜色跟随页面主题（浅/深），
        //     背景就是页面自己的颜色，视觉上真正连成一片。
        setupEdgeToEdge();

        // 引擎初始化：数据目录走 App 私有区，卸载即清空
        Engine.ensureStarted(this);

        web = new WebView(this);
        // WebView 自己画背景（跟随主题），避免透明处露出 Activity 的默认白底
        web.setBackgroundColor(0x00000000);
        setContentView(web);

        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setAllowFileAccess(false);
        s.setAllowContentAccess(false);
        s.setCacheMode(WebSettings.LOAD_NO_CACHE);

        web.setWebViewClient(new WebViewClient() {
            @Override
            public boolean shouldOverrideUrlLoading(WebView v, android.webkit.WebResourceRequest req) {
                String url = req.getUrl().toString();
                if (url.startsWith("points://")) {
                    handlePointsScheme(url);
                    return true;
                }
                return false;
            }
        });

        // 面板里的「添加账号」按钮通过这个桥拉起原生登录容器
        web.addJavascriptInterface(new Bridge(), "PointsBridge");

        // 页面加载完后把主题同步给系统栏（页面自己也会主动调 setTheme）
        web.setWebChromeClient(new android.webkit.WebChromeClient() {
            @Override
            public void onProgressChanged(WebView view, int p) {
                if (p == 100) view.evaluateJavascript(
                        "window.__pointsTheme || 'dark'", value -> applySystemBarTheme(value));
            }
        });

        waitThenLoad(0);

        // 0.6.4：壳层不再触发任何签到。打开 App 后的「签 or 不签」由页面
        // 按引擎侧的今日记录判定（/api/daily），壳层只负责显示。

        // 0.6.4：不再启动常驻前台服务。
        //
        // 原来这里拉起 CheckinService 做保活，通知栏常挂一条「已开启自动签到」。
        // 用户的要求是「不要天天动不动就给我去签到」，常驻服务本身就是那个
        // 「动不动」的来源：它在后台活着，用户永远不知道它什么时候又跑了一轮。
        // 现在没有任何后台任务，不签到、不刷新、不保活。
    }

    /**
     * 全面屏：系统栏透明 + WebView 铺到系统栏下面。
     *
     * Android 15(targetSdk 35) 起强制 edge-to-edge，这里在 26~34 上手写等价效果，
     * 行为一致，不用分版本各写一套。
     */
    private void setupEdgeToEdge() {
        android.view.Window w = getWindow();
        // 系统栏（状态栏 + 导航栏）透明，内容才能铺到底
        w.setStatusBarColor(android.graphics.Color.TRANSPARENT);
        w.setNavigationBarColor(android.graphics.Color.TRANSPARENT);
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.P) {
            // 刘海/挖孔屏：允许内容延伸进安全区，否则上面会留一条黑
            w.getAttributes().layoutInDisplayCutoutMode =
                    android.view.WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
        }
        // 让 WebView 铺到系统栏之下（真正的全屏）
        w.getDecorView().setSystemUiVisibility(
                android.view.View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                        | android.view.View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                        | android.view.View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION);
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.Q) {
            // 系统自动给状态栏/导航栏加半透明遮罩会让底色发灰，关掉才是纯页面色
            w.setStatusBarContrastEnforced(false);
            w.setNavigationBarContrastEnforced(false);
        }
    }

    /**
     * 按页面实际主题设置系统栏图标的明暗。
     *
     * 背景是透明的（就是页面自己），所以只需要决定图标用深色还是浅色：
     * 浅色主题 → 深色图标；深色主题 → 浅色图标。之前完全没设，系统按
     * Theme.DeviceDefault.Light 给了深色图标，切到深色主题后就「看不清」了。
     */
    private void applySystemBarTheme(String theme) {
        // evaluateJavascript 的回调值是 JSON 字面量，字符串会带一对引号
        // （传 "dark" 回来的是 "\"dark\""）。不剥掉引号，"dark".equals(...)
        // 永远为 false，状态栏图标就一直是浅色底配白字，看不清。
        String t = (theme == null) ? "" : theme.replace("\"", "").trim();
        boolean dark = "dark".equals(t);
        android.view.Window w = getWindow();
        android.view.View v = w.getDecorView();
        int flags = v.getSystemUiVisibility();
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.R) {
            // API 30+：用 WindowInsetsController，语义最清晰
            android.view.WindowInsetsController c = w.getInsetsController();
            if (c != null) {
                int mask = android.view.WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS
                        | android.view.WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS;
                c.setSystemBarsAppearance(dark ? 0 : mask, mask);
            }
        } else {
            if (dark) {
                flags &= ~(android.view.View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR
                        | android.view.View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR);
            } else {
                flags |= android.view.View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR
                        | android.view.View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR;
            }
            // 保持全屏布局标志，只改图标明暗
            flags |= android.view.View.SYSTEM_UI_FLAG_LAYOUT_STABLE
                    | android.view.View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                    | android.view.View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION;
            v.setSystemUiVisibility(flags);
        }
    }

    /** 轮询本机面板端口，就绪后加载。最多等 3 秒。 */
    private void waitThenLoad(int tries) {
        new Thread(() -> {
            boolean up = ping();
            ui.post(() -> {
                if (up) {
                    web.loadUrl(URL_BASE + "/");
                } else if (tries < 30) {
                    ui.postDelayed(() -> waitThenLoad(tries + 1), 100);
                } else {
                    Toast.makeText(this, "引擎启动超时，请重开 App", Toast.LENGTH_LONG).show();
                }
            });
        }).start();
    }

    private boolean ping() {
        HttpURLConnection c = null;
        try {
            c = (HttpURLConnection) new URL(URL_BASE + "/api/health").openConnection();
            c.setConnectTimeout(300);
            c.setReadTimeout(300);
            return c.getResponseCode() == 200;
        } catch (IOException e) {
            return false;
        } finally {
            if (c != null) {
                c.disconnect();
            }
        }
    }

    /** 面板 JS 调用的桥。方法名与 panel/index.go 里的 window.PointsBridge 对齐。 */
    private final class Bridge {
        /** 打开某平台的官方登录页。 */
        @android.webkit.JavascriptInterface
        public void openLogin(final String provider) {
            ui.post(() -> {
                if (provider == null || provider.isEmpty()) {
                    // 面板没传平台时，用第一个已支持平台兜底
                    String[] all = LoginUrls.allProviders();
                    startLogin(all.length > 0 ? all[0] : "workbuddy");
                } else {
                    startLogin(provider);
                }
            });
        }

        /** 登录完成后强制刷新面板。 */
        @android.webkit.JavascriptInterface
        public void reload() {
            ui.post(() -> { if (web != null) web.reload(); });
        }

        /**
         * 把备份内容存成文件：弹系统「保存到」面板（SAF），用户自己选位置。
         *
         * 为什么不用 App 私有目录直接写：用户拿不出去。SAF 让文件真正落到
         * 「下载 / 文件」里，换机、重装后还能再导回来。
         */
        @android.webkit.JavascriptInterface
        public void saveFile(final String name, final String content) {
            ui.post(() -> {
                try {
                    Intent i = new Intent(Intent.ACTION_CREATE_DOCUMENT);
                    i.addCategory(Intent.CATEGORY_OPENABLE);
                    i.setType("application/json");
                    i.putExtra(Intent.EXTRA_TITLE, name);
                    pendingSaveContent = content;
                    startActivityForResult(i, SAVE_REQ);
                } catch (Throwable e) {
                    Toast.makeText(MainActivity.this, "无法打开保存面板", Toast.LENGTH_SHORT).show();
                }
            });
        }

        /** 选一个备份文件：弹系统文件选择器，读回文本后交给页面。 */
        @android.webkit.JavascriptInterface
        public void pickFile() {
            ui.post(() -> {
                try {
                    Intent i = new Intent(Intent.ACTION_OPEN_DOCUMENT);
                    i.addCategory(Intent.CATEGORY_OPENABLE);
                    i.setType("*/*");
                    i.putExtra(Intent.EXTRA_MIME_TYPES,
                            new String[]{"application/json", "text/plain", "application/octet-stream"});
                    startActivityForResult(i, PICK_REQ);
                } catch (Throwable e) {
                    Toast.makeText(MainActivity.this, "无法打开文件选择器", Toast.LENGTH_SHORT).show();
                }
            });
        }

        /**
         * 面板切换主题后调用："dark" / "light"。
         * 状态栏背景是透明的（页面自己画），这里只改图标明暗。
         */
        @android.webkit.JavascriptInterface
        public void setTheme(final String theme) {
            ui.post(() -> applySystemBarTheme(theme));
        }
    }

    /** 待写入的内容（SAF 回调里才知道目标 Uri，先暂存）。 */
    private String pendingSaveContent = null;

    /** 写文本到 SAF 返回的 Uri。 */
    private void writeToUri(android.net.Uri uri, String content) {
        try (java.io.OutputStream os = getContentResolver().openOutputStream(uri, "wt")) {
            if (os == null) throw new IOException("openOutputStream 返回 null");
            os.write(content.getBytes(java.nio.charset.StandardCharsets.UTF_8));
            os.flush();
            Toast.makeText(this, "已保存", Toast.LENGTH_SHORT).show();
        } catch (Throwable e) {
            Toast.makeText(this, "保存失败：" + e.getMessage(), Toast.LENGTH_LONG).show();
        }
    }

    /** 读 SAF 选中的文件，回传给页面。 */
    private void readFromUri(android.net.Uri uri) {
        try (java.io.InputStream is = getContentResolver().openInputStream(uri)) {
            if (is == null) throw new IOException("openInputStream 返回 null");
            java.io.ByteArrayOutputStream bos = new java.io.ByteArrayOutputStream();
            byte[] buf = new byte[8192];
            int n;
            while ((n = is.read(buf)) > 0) bos.write(buf, 0, n);
            String text = new String(bos.toByteArray(), java.nio.charset.StandardCharsets.UTF_8);
            // 转成 JSON 字符串字面量再注入，避免换行/引号把 JS 打断
            String js = "window.__pointsOnFilePicked && window.__pointsOnFilePicked("
                    + org.json.JSONObject.quote(text) + ")";
            if (web != null) web.evaluateJavascript(js, null);
        } catch (Throwable e) {
            Toast.makeText(this, "读取失败：" + e.getMessage(), Toast.LENGTH_LONG).show();
        }
    }

    /** 打开某平台的登录页。provider 决定加载哪个官网。 */
    public void startLogin(String provider) {
        String url = LoginUrls.forProvider(provider);
        if (url == null) {
            Toast.makeText(this, "未知平台: " + provider, Toast.LENGTH_SHORT).show();
            return;
        }
        // 换账号前清活会话，避免把上一个账号的 cookie 带进来
        CookieJar.clear();
        Intent i = new Intent(this, LoginActivity.class);
        i.putExtra(LoginActivity.EXTRA_PROVIDER, provider);
        i.putExtra(LoginActivity.EXTRA_URL, url);
        startActivityForResult(i, LOGIN_REQ);
    }

    @Override
    protected void onResume() {
        super.onResume();
        if (web == null) return;
        awayAt = 0;

        // 0.6.4：这里原有的三件事全部删除（用户要求：「这6个触发全部删掉」）。
        //
        //   1. 首次 onResume 注入 __pointsAutoCheckin()   → 删
        //   2. 后台 >45s 就 rebuild() 整页重建             → 删
        //   3. refreshOrRebuild()：注入探活 + .card,.row 探测，读不到就重建 → 删
        //
        // 为什么全删：这些都是「回到前台」这个动作上的自动行为。签到部分会让
        // 一天开几次 App 就签几轮（用户原话「容易黑号」）；重建部分是冲着
        // 「读取失败」去的，但真因在引擎侧复用了死连接（已改成短连接），
        // 重建只会把页面刷白，实质什么都没改变。
        //
        // 现在回前台只做一件事：让页面自己刷新一次列表。
        // 是否签到由页面按「今天跑过没有」决定，壳层不参与判断。
        ui.postDelayed(() -> {
            if (web != null) web.evaluateJavascript(
                    "window.__pointsRefresh && window.__pointsRefresh()", null);
        }, 300);
    }

    /** 整页重建：先 about:blank 再回面板，确保渲染进程真的换了新的。 */
    private void rebuild() {
        if (web == null) return;
        // 直接 reload 有时会复用同一个已损坏的渲染进程，先清空再载入才可靠
        web.loadUrl("about:blank");
        ui.postDelayed(() -> {
            if (web != null) web.loadUrl(URL_BASE + "/");
        }, 120);
    }

    @Override
    protected void onPause() {
        super.onPause();
        awayAt = System.currentTimeMillis();
    }

    @Override
    protected void onActivityResult(int req, int res, Intent data) {
        super.onActivityResult(req, res, data);
        // 备份相关：SAF 保存 / 读取
        if (req == SAVE_REQ) {
            if (res == RESULT_OK && data != null && data.getData() != null && pendingSaveContent != null) {
                writeToUri(data.getData(), pendingSaveContent);
            }
            pendingSaveContent = null;
            return;
        }
        if (req == PICK_REQ) {
            if (res == RESULT_OK && data != null && data.getData() != null) {
                readFromUri(data.getData());
            } else {
                // 用户取消：清掉回调，避免页面一直挂着等
                if (web != null) web.evaluateJavascript(
                        "try{window.__pointsOnFilePicked=null}catch(e){}", null);
            }
            return;
        }
        if (req == LOGIN_REQ) {
            // 只有用户点了「完成」才提交；点「取消」/ 返回键时 LoginActivity
            // 已自行清掉快照，这里绝不能无条件 commit（会凭空多出一个账号）。
            if (res == RESULT_OK) {
                Engine.commitPendingLogins(this);
            } else {
                // 取消：清掉 OAuth 待办标记，避免 reload 后白白轮询 60 秒
                web.evaluateJavascript("try{localStorage.removeItem('oauth_pending')}catch(e){}", null);
            }
            rebuild();
        }
    }

    /** 自定义 scheme 兜底：面板在没有 PointsBridge 时会跳 points://login?provider=xxx */
    private void handlePointsScheme(String url) {
        try {
            android.net.Uri u = android.net.Uri.parse(url);
            if ("points".equals(u.getScheme()) && "login".equals(u.getHost())) {
                String p = u.getQueryParameter("provider");
                if (p == null || p.isEmpty()) {
                    String[] all = LoginUrls.allProviders();
                    p = all.length > 0 ? all[0] : "workbuddy";
                }
                startLogin(p);
            }
        } catch (Throwable ignored) {
        }
    }

    @Override
    public void onBackPressed() {
        if (web != null && web.canGoBack()) {
            web.goBack();
        } else {
            super.onBackPressed();
        }
    }
}
