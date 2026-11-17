package co.repo.points;

/** 各平台登录入口。改这里就能换登录页，不涉及任何业务逻辑。 */
final class LoginUrls {

    private LoginUrls() {}

    static String forProvider(String id) {
        switch (id == null ? "" : id) {
            case "workbuddy":
                return "https://www.workbuddy.cn/";
            case "lingxi":
                return "https://www.kdocs.cn/";
            case "dumate":
                return "https://wenxin.baidu.com/";
            default:
                return null;
        }
    }
}
