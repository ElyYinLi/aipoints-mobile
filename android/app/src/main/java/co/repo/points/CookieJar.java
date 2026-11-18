package co.repo.points;

import android.content.Context;
import android.content.SharedPreferences;
import android.net.Uri;
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

    /**
     * 把所有已接入域下的 cookie 一次性导出。
     *
     * 关键点：多个站点的 cookie 绝不能简单用 "; " 拼在一起——不同站点常出现同名项
     * （session、uid 之类），拼完再整体发给某一个站点，会把别的站点的值一起送出去，
     * 既可能串号，也可能让服务端取到错误的值。这里按域名分组，用换行分隔，
     * 由 engine 侧只挑目标域命中的那一组使用。
     */
    static String dumpAll() {
        try {
            CookieManager cm = CookieManager.getInstance();
            StringBuilder sb = new StringBuilder();
            for (String domain : DOMAINS) {
                String ck = cm.getCookie(domain);
                if (ck == null || !ck.contains("=")) {
                    continue;
                }
                if (sb.length() > 0) {
                    sb.append("\n");
                }
                sb.append(domain).append(" ").append(ck);
            }
            return sb.length() == 0 ? null : sb.toString();
        } catch (Throwable t) {
            return null;
        }
    }

    /** 全部已接入平台的 cookie 域，新增平台只改这里。 */
    static final String[] DOMAINS = {
            ".workbuddy.cn",
            ".kdocs.cn",
            ".baidu.com",
            ".trae.cn",
            ".trustoken.cn",
    };

    /** 取单个域当前的 cookie 串（登录页每加载完成时调用）。 */
    static String domainCookies(String url) {
        try {
            Uri u = Uri.parse(url);
            String host = u.getHost();
            if (host == null) {
                return dump(url);
            }
            // 顶级域 + 父域都试一遍：cookie 的 Domain 属性可能落在任一层级
            String ck = CookieManager.getInstance().getCookie(host);
            int dot = host.indexOf('.');
            if (dot > 0) {
                String parent = host.substring(dot);
                String ck2 = CookieManager.getInstance().getCookie(parent);
                if (ck2 != null && !ck2.isEmpty()) {
                    ck = (ck == null || ck.isEmpty()) ? ck2 : ck + "; " + ck2;
                }
            }
            return ck;
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
