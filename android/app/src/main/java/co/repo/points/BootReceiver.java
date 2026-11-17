package co.repo.points;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;

/** 开机自启：重启手机后把前台服务拉起来，否则自动签到会静默失效。 */
public final class BootReceiver extends BroadcastReceiver {

    @Override
    public void onReceive(Context ctx, Intent intent) {
        if (Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction())) {
            CheckinService.start(ctx);
        }
    }
}
