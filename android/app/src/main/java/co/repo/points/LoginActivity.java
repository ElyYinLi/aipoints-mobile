package co.repo.points;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;

/**
 * 登录容器：用系统 WebView 打开平台官网，让用户在真正的官方页面上登录。
 *
 * 为什么不用内嵌 HTTP 抓包：我们只需要 cookie / token，而 WebView 的
 * CookieManager 能直接读到它们。这样：
 *   1. 不碰用户的账号密码（密码只流向平台服务器）；
 *   2. 平台侧看到的是一次完全正常的网页登录，风控友好；
 *   3. 无需在 App 里实现任何 OAuth 细节，平台改流程也不影响我们。
 *
 * 登录成功后由 MainActivity 调用 /api/login/done 把 cookie 交给 Go 引擎。
 */
public final class LoginActivity extends Activity {

    public static final String EXTRA_PROVIDER = "provider";
    public static final String EXTRA_URL = "url";

    private WebView web;

    @SuppressLint("SetJavaScriptEnabled")
    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);

        String provider = getIntent().getStringExtra(EXTRA_PROVIDER);
        String url = getIntent().getStringExtra(EXTRA_URL);
        if (provider == null || url == null) {
            finish();
            return;
        }

        web = new WebView(this);
        setContentView(web);

        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setDatabaseEnabled(true);
        s.setUserAgentString(s.getUserAgentString());
        s.setMixedContentMode(WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE);

        web.setWebViewClient(new WebViewClient() {
            @Override
            public boolean shouldOverrideUrlLoading(WebView v, WebResourceRequest req) {
                Uri u = req.getUrl();
                // 只允许 http/https，避免外部 scheme 把用户带走
                String scheme = u.getScheme();
                return !("http".equals(scheme) || "https".equals(scheme));
            }

            @Override
            public void onPageFinished(WebView v, String u) {
                // 每次页面加载完成就探测一次：很多平台登录成功后会跳到工作台，
                // 此时 cookie 已经就位，不必等用户手动点「完成」。
                String cookies = CookieJar.dump(u);
                if (cookies != null && cookies.contains("=") && cookies.length() > 40) {
                    CookieJar.remember(LoginActivity.this, provider, u, cookies);
                }
            }
        });

        web.loadUrl(url);
    }

    @Override
    public void onBackPressed() {
        if (web != null && web.canGoBack()) {
            web.goBack();
        } else {
            // 返回即视为「登录完成」，由 MainActivity 取最新 cookie 落库
            setResult(RESULT_OK, new Intent());
            finish();
        }
    }

    @Override
    protected void onDestroy() {
        if (web != null) {
            web.destroy();
            web = null;
        }
        super.onDestroy();
    }
}
