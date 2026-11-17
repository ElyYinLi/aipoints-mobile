package co.repo.points;

import android.content.Context;
import android.content.SharedPreferences;
import android.webkit.CookieManager;

/**
 * Cookie 隔离的最小实现。
 *
 * 需求：同一平台可能有多个账号，绝不能互相串 cookie。
 * 做法：每登录完一个账号，把它当时的全站 cookie 串快照下来，
 *      以 provider + 账号 id 为 key 存进本地（加密交给 KeyVault）；
 *      引擎发起请求时按 key 取串、塞进 HTTP 头，全程不依赖 WebView 的活会话。
 *
 * 这样即使用户随后在 WebView 里换成另一个账号，已保存账号的凭据也不受影响。
 */
final class CookieJar {

    private static final String PREF = "cookie_jar";

    private CookieJar() {}

    /** 读取某 URL 当前在 WebView 里的全部 cookie（含 HttpOnly）。 */
    static String dump(String url) {
        try {
            CookieManager cm = CookieManager.getInstance();
            return cm.getCookie(url);
        } catch (Throwable t) {
            return null;
        }
    }

    /** 把所有域下的 cookie 一次性导出（部分平台跨域，单 URL 取不全）。 */
    static String dumpAll() {
        try {
            CookieManager cm = CookieManager.getInstance();
            return cm.getCookie(".workbuddy.cn") + "; " + cm.getCookie(".kdocs.cn") + "; " + cm.getCookie(".baidu.com");
        } catch (Throwable t) {
            return null;
        }
    }

    /** 记录一次登录结果，等待 MainActivity 提交给引擎。 */
    static void remember(Context ctx, String provider, String url, String cookies) {
        prefs(ctx).edit()
                .putString("pending:" + provider, cookies)
                .apply();
    }

    /** 取出并清掉待提交的 cookie。 */
    static String takePending(Context ctx, String provider) {
        SharedPreferences p = prefs(ctx);
        String v = p.getString("pending:" + provider, null);
        if (v != null) {
            p.edit().remove("pending:" + provider).apply();
        }
        return v;
    }

    /** 清掉 WebView 的活会话，用于「换账号」前避免串号。 */
    static void clear() {
        try {
            CookieManager cm = CookieManager.getInstance();
            cm.removeAllCookies(null);
            cm.flush();
        } catch (Throwable ignored) {
        }
    }

    private static SharedPreferences prefs(Context ctx) {
        return ctx.getSharedPreferences(PREF, Context.MODE_PRIVATE);
    }
}
