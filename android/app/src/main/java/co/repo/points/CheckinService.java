package co.repo.points;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.os.Build;
import android.os.IBinder;

/**
 * 前台服务：保活 + 到点触发签到。
 *
 * 为什么必须用前台服务：Android 8 之后后台进程会被随时冻结，Doze 模式下
 * 网络直接被掐。要在「用户不开 App 的时候也能签到」，只有前台服务 + 常驻通知
 * 这一条路是稳的（WorkManager 最小间隔 15 分钟，且不保证准时）。
 *
 * 通知文案刻意低调：只写「积分矩阵运行中」，不暴露平台名。
 */
public final class CheckinService extends Service {

    private static final String CH = "points_bg";
    private static final int NID = 0x9151;

    static void start(Context ctx) {
        Intent i = new Intent(ctx, CheckinService.class);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            ctx.startForegroundService(i);
        } else {
            ctx.startService(i);
        }
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        Engine.ensureStarted(this);

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationManager nm = getSystemService(NotificationManager.class);
            NotificationChannel ch = new NotificationChannel(
                    CH, "后台签到", NotificationManager.IMPORTANCE_MIN);
            ch.setShowBadge(false);
            nm.createNotificationChannel(ch);
        }

        Notification n = new Notification.Builder(this, CH)
                .setContentTitle("积分矩阵运行中")
                .setContentText("已开启自动签到")
                .setSmallIcon(android.R.drawable.stat_notify_sync)
                .setOngoing(true)
                .build();

        startForeground(NID, n);
        return START_STICKY;
    }

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }
}
