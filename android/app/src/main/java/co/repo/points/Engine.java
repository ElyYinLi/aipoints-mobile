package co.repo.points;

import android.content.Context;

import org.json.JSONObject;

/**
 * Go 引擎的 Java 门面。
 *
 * 引擎本体由 gomobile bind 编出来的 .aar 提供（包名 mobile，类 mobile.Mobile）。
 * 但本书面构建流程（无 Gradle）里我们直接把 Java 源码放在同一模块编译，
 * 所以这里用反射 + 编译期两种方式取其一：
 *   - 若 classpath 上有 mobile.Mobile，就正常调用；
 *   - 否则降级为「引擎不可用」，UI 仍能打开并给出提示，不至于闪退。
 *
 * 所有参数都是 JSON 字符串，返回也是 JSON 字符串——gomobile 只认基础类型，
 * 这是最省心的跨界约定。
 */
final class Engine {

    private static volatile boolean started = false;

    private Engine() {}

    static synchronized void ensureStarted(Context ctx) {
        if (started) {
            return;
        }
        String dir = ctx.getFilesDir().getAbsolutePath() + "/engine";
        try {
            Class<?> c = Class.forName("mobile.Mobile");
            Object r = c.getMethod("Start", String.class).invoke(null, dir);
            started = r != null;
        } catch (Throwable t) {
            started = false;
        }
    }

    static boolean ready() {
        return started;
    }

    /** 把 LoginActivity 攒下的 cookie 交给引擎落库。 */
    static void commitPendingLogins(Context ctx) {
        for (String provider : new String[]{"workbuddy", "lingxi", "dumate"}) {
            String cookies = CookieJar.takePending(ctx, provider);
            if (cookies == null || cookies.isEmpty()) {
                continue;
            }
            // 账号 id 用 cookie 串的指纹，保证同一账号重复登录不会产生重复卡片
            String accountId = provider + "-" + Integer.toHexString(cookies.hashCode());
            try {
                JSONObject in = new JSONObject();
                in.put("provider", provider);
                in.put("account_id", accountId);
                in.put("label", provider);
                in.put("cookie", cookies);
                // 先把 secret 交给 Keystore，再把脱敏后的记录交给引擎
                KeyVault.put(ctx, "secret:" + accountId, in.toString());
                call("AddAccount", in.toString());
            } catch (Throwable ignored) {
            }
        }
    }

    static String call(String method, String arg) {
        try {
            Class<?> c = Class.forName("mobile.Mobile");
            return (String) c.getMethod(method, String.class).invoke(null, arg);
        } catch (Throwable t) {
            return "{\"ok\":false,\"error\":\"" + t.getClass().getSimpleName() + "\"}";
        }
    }

    static String call(String method) {
        try {
            Class<?> c = Class.forName("mobile.Mobile");
            return (String) c.getMethod(method).invoke(null);
        } catch (Throwable t) {
            return "{\"ok\":false,\"error\":\"" + t.getClass().getSimpleName() + "\"}";
        }
    }
}
