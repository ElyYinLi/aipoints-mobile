package co.repo.points;

import android.annotation.SuppressLint;
import android.app.Activity;
import android.content.Intent;
import android.graphics.Color;
import android.net.Uri;
import android.os.Bundle;
import android.util.TypedValue;
import android.view.Gravity;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

/**
 * 登录容器：用系统 WebView 打开平台官网，让用户在真正的官方页面上登录。
 *
 * 【重要】不做「自动判定登录成功」。
 * 之前的实现在 onPageFinished 里看 URL + cookie 长度就自动 finish()，而官网
 * 首页本身就带一堆埋点 cookie，刚打开就被判定为已登录直接回跳，用户根本
 * 来不及找登录入口。现在只有用户点顶部「完成」才提取 cookie 并返回。
 *
 * 用户也可以点「取消」退出，或用返回键在页面栈里后退；
 * 这两种情况都不会写入账号（会清掉待提交的快照）。
 */
public final class LoginActivity extends Activity {

    public static final String EXTRA_PROVIDER = "provider";
    public static final String EXTRA_URL = "url";

    private WebView web;
    private String provider;
    private String startUrl;

    @SuppressLint("SetJavaScriptEnabled")
    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);

        provider = getIntent().getStringExtra(EXTRA_PROVIDER);
        startUrl = getIntent().getStringExtra(EXTRA_URL);
        if (provider == null || startUrl == null) {
            finish();
            return;
        }

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setBackgroundColor(Color.parseColor("#FBF8FF"));
        root.setFitsSystemWindows(true);

        root.addView(buildToolbar());
        web = new WebView(this);
        root.addView(web, new LinearLayout.LayoutParams(
                LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f));
        setContentView(root);

        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setDatabaseEnabled(true);
        s.setMixedContentMode(WebSettings.MIXED_CONTENT_COMPATIBILITY_MODE);

        web.setWebViewClient(new WebViewClient() {
            @Override
            public boolean shouldOverrideUrlLoading(WebView v, WebResourceRequest req) {
                Uri u = req.getUrl();
                // 只允许 http/https，避免外部 scheme 把用户带走
                String scheme = u.getScheme();
                return !("http".equals(scheme) || "https".equals(scheme));
            }
            // 刻意不做 onPageFinished 自动收尾：登录是否完成由用户判断
        });

        web.loadUrl(startUrl);
    }

    private LinearLayout buildToolbar() {
        LinearLayout bar = new LinearLayout(this);
        bar.setOrientation(LinearLayout.HORIZONTAL);
        bar.setGravity(Gravity.CENTER_VERTICAL);
        bar.setBackgroundColor(Color.parseColor("#EEEAFA"));
        int pad = dp(10);
        int sb = statusBarHeight();
        bar.setPadding(pad * 2, sb + pad, pad, pad);

        TextView title = new TextView(this);
        title.setText("登录完成后点「完成」");
        title.setTextSize(TypedValue.COMPLEX_UNIT_SP, 14);
        title.setTextColor(Color.parseColor("#5A5866"));
        bar.addView(title, new LinearLayout.LayoutParams(0,
                LinearLayout.LayoutParams.WRAP_CONTENT, 1f));

        Button cancel = flatButton("取消");
        cancel.setOnClickListener(v -> cancelLogin());
        bar.addView(cancel);

        Button done = flatButton("完成");
        done.setTextColor(Color.parseColor("#4F5BD5"));
        done.setOnClickListener(v -> done());
        bar.addView(done);
        return bar;
    }

    private Button flatButton(String text) {
        Button b = new Button(this);
        b.setText(text);
        b.setAllCaps(false);
        b.setTextSize(TypedValue.COMPLEX_UNIT_SP, 15);
        b.setBackgroundColor(Color.TRANSPARENT);
        b.setTextColor(Color.parseColor("#5A5866"));
        b.setPadding(dp(14), dp(8), dp(14), dp(8));
        return b;
    }

    private int dp(int v) {
        return (int) TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v,
                getResources().getDisplayMetrics());
    }

    private int statusBarHeight() {
        int id = getResources().getIdentifier("status_bar_height", "dimen", "android");
        return id > 0 ? getResources().getDimensionPixelSize(id) : 0;
    }

    /** 只在用户明确点「完成」时调用：取当前页面的 cookie 交给引擎。 */
    private void done() {
        if (isFinishing()) {
            return;
        }
        String url = (web != null && web.getUrl() != null) ? web.getUrl() : startUrl;
        String ck = CookieJar.domainCookies(url);
        if (ck == null || !ck.contains("=")) {
            Toast.makeText(this, "没读到登录信息，请确认已登录后再点「完成」", Toast.LENGTH_LONG).show();
            return;
        }
        CookieJar.remember(this, provider, url, ck);
        setResult(RESULT_OK, new Intent());
        finish();
    }

    /** 取消：清掉可能残留的快照，MainActivity 不会提交账号。 */
    private void cancelLogin() {
        CookieJar.takePending(this, provider);
        setResult(RESULT_CANCELED, new Intent());
        finish();
    }

    @Override
    public void onBackPressed() {
        if (web != null && web.canGoBack()) {
            web.goBack();
        } else {
            cancelLogin();
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
