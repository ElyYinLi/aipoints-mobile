package co.repo.points;

import org.json.JSONObject;

/**
 * Go 引擎的 Java 门面。
 *
 * 引擎由 gomobile bind 编出来的 Mobile.aar 提供：类 `mobile.Mobile`，方法名是
 * 被 gomobile 转成的首字母小写驼峰（Go 侧的 Start → start、CheckinOne →
 * checkinOne）。所有参数与返回值都是 JSON 字符串——gomobile 只认基础类型。
 *
 * 为什么要 `Engine.touch()`：gomobile 生成类里有个空方法 touch()，唯一作用是
 * 让静态初始化块（加载 libgojni.so）被触发。不调它，第一次调业务方法时才加载
 * 也不会崩，但提前调用能把「so 找不到」这类错误尽早暴露在本类里。
 */
final class Engine {

    private static volatile boolean started = false;
    private static volatile String lastError = "";

    private Engine() {}

    static synchronized void ensureStarted(android.content.Context ctx) {
        if (started) {
            return;
        }
        String dir = ctx.getFilesDir().getAbsolutePath() + "/engine";
        try {
            mobile.Mobile.touch();
            String r = mobile.Mobile.start(dir);
            started = r != null && r.contains("\"ok\":true");
            if (!started) {
                lastError = r;
            }
        } catch (Throwable t) {
            started = false;
            lastError = t.getClass().getSimpleName() + ": " + t.getMessage();
        }
    }

    static boolean ready() {
        return started;
    }

    static String lastError() {
        return lastError;
    }

    /**
     * 把 LoginActivity 攒下的 cookie 交给引擎落库。
     *
     * 两件事：先把完整凭据密文存进 Keystore（KeyVault），再把同样的明文交给
     * Go 引擎（Go 侧 FileStore 在 App 私有目录，权限由 Android 沙箱保证）。
     * 移动端真正长期保存的是 KeyVault 那份；引擎侧那份用于进程内直接读取，
     * App 卸载即随私有目录消失。
     */
    static void commitPendingLogins(android.content.Context ctx) {
        for (String provider : LoginUrls.allProviders()) {
            String cookies = CookieJar.takePending(ctx, provider);
            if (cookies == null || cookies.isEmpty()) {
                continue;
            }
            String accountId = provider + "-" + Integer.toHexString(cookies.hashCode());
            try {
                JSONObject in = new JSONObject();
                in.put("provider", provider);
                in.put("account_id", accountId);
                in.put("label", provider);
                in.put("cookie", cookies);
                String payload = in.toString();
                KeyVault.put(ctx, "secret:" + accountId, payload);
                mobile.Mobile.addAccount(payload);
            } catch (Throwable t) {
                lastError = t.getClass().getSimpleName();
            }
        }
    }

    static String queryAll() {
        try {
            return mobile.Mobile.queryAll();
        } catch (Throwable t) {
            return err(t);
        }
    }

    static String checkinOne(String provider, String accountId) {
        try {
            return mobile.Mobile.checkinOne(provider, accountId);
        } catch (Throwable t) {
            return err(t);
        }
    }

    static String checkinAll() {
        try {
            return mobile.Mobile.checkinAll();
        } catch (Throwable t) {
            return err(t);
        }
    }

    static String summary() {
        try {
            return mobile.Mobile.summary();
        } catch (Throwable t) {
            return err(t);
        }
    }

    private static String err(Throwable t) {
        return "{\"ok\":false,\"error\":\"" + t.getClass().getSimpleName() + "\"}";
    }
}
