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

    private WebView web;
    private final Handler ui = new Handler(Looper.getMainLooper());
    /** 首次进入前台时自动签到一次；从登录页返回时不重复触发。 */
    private boolean autoCheckinOnResume = false;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);

        // 引擎初始化：数据目录走 App 私有区，卸载即清空
        Engine.ensureStarted(this);

        web = new WebView(this);
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

        waitThenLoad(0);

        // 「打开软件即签到」：进前台就自动跑一次，不依赖任何后台服务或定时器。
        // 用户明确说过：不需要静默准点，我自己会定时开 App。这就是最贴合的实现。
        autoCheckinOnResume = true;

        // 前台服务：让定时签到在息屏后仍有机会触发
        CheckinService.start(this);
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
        if (autoCheckinOnResume && web != null) {
            autoCheckinOnResume = false;
            // 交给页面自己的逻辑：它会先查余额，只对未签的账号发起签到，
            // 已签的不会重复请求（省流量也降低风控概率）。
            ui.postDelayed(() -> web.evaluateJavascript(
                    "window.__pointsAutoCheckin && window.__pointsAutoCheckin()", null), 900);
        }
    }

    @Override
    protected void onActivityResult(int req, int res, Intent data) {
        super.onActivityResult(req, res, data);
        if (req == LOGIN_REQ) {
            Engine.commitPendingLogins(this);
            web.reload();
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
