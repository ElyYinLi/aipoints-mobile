package co.repo.points;

/** 各平台登录入口。改这里就能换登录页，不涉及任何业务逻辑。 */
final class LoginUrls {

    private LoginUrls() {}

    /** 全部已支持平台，顺序即 UI 顺序。 */
    static String[] allProviders() {
        return new String[]{"workbuddy", "dumate", "aipy", "lingxi", "trae"};
    }

    static String forProvider(String id) {
        switch (id == null ? "" : id) {
            case "workbuddy":
                // token 型：本机 302 到官方 OAuth 授权页，登录完由引擎轮询换 token
                return "http://127.0.0.1:7879/login/workbuddy";
            case "lingxi":
                return "https://www.kdocs.cn/";
            case "dumate":
                // 业务站是 dumate.baidu.com；wenxin.baidu.com 是文心一言，没有登录入口
                return "https://dumate.baidu.com/";
            case "trae":
                return "https://www.trae.cn/";
            case "aipy":
                // Trustoken 用户中心：登录后 cookie 落在 .trustoken.cn
                return "https://www.trustoken.cn/";
            default:
                return null;
        }
    }
}
