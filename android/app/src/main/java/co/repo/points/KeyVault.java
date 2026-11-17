package co.repo.points;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.Base64;

import java.security.KeyStore;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/**
 * 凭据本地加密：Android Keystore 里的 AES-256-GCM 密钥，永不导出。
 *
 * 设计取舍：
 *   - 密钥在 Keystore（TEE / StrongBox），只能在本 App 里用来加解密；
 *   - 数据放 SharedPreferences，密文 = IV(12) || ciphertext || tag(16)，整体 base64；
 *   - 不用 EncryptedSharedPreferences：那要拉 androidx.security 整套依赖，
 *     和我们「零依赖、无 Gradle」的构建路线冲突。
 *
 * 与桌面版的对比：桌面版把 cookie 明文写 accounts.json，这是被逆向报告点名的
 * 安全缺陷。这里不重复那个错误。
 */
final class KeyVault {

    private static final String PREF = "vault";
    private static final String ALIAS = "points_master_key";
    private static final String TRANSFORM = "AES/GCM/NoPadding";
    private static final int IV_LEN = 12;
    private static final int TAG_BITS = 128;

    private KeyVault() {}

    static void put(Context ctx, String key, String plain) {
        if (plain == null) {
            return;
        }
        try {
            byte[] enc = encrypt(plain.getBytes("UTF-8"));
            prefs(ctx).edit()
                    .putString(key, Base64.encodeToString(enc, Base64.NO_WRAP))
                    .apply();
        } catch (Throwable t) {
            // 加密失败时宁可丢数据也不落明文
        }
    }

    static String get(Context ctx, String key) {
        String b64 = prefs(ctx).getString(key, null);
        if (b64 == null) {
            return null;
        }
        try {
            byte[] plain = decrypt(Base64.decode(b64, Base64.NO_WRAP));
            return new String(plain, "UTF-8");
        } catch (Throwable t) {
            return null;
        }
    }

    static void remove(Context ctx, String key) {
        prefs(ctx).edit().remove(key).apply();
    }

    private static byte[] encrypt(byte[] plain) throws Exception {
        Cipher c = Cipher.getInstance(TRANSFORM);
        c.init(Cipher.ENCRYPT_MODE, key());
        byte[] iv = c.getIV();
        byte[] ct = c.doFinal(plain);
        byte[] out = new byte[iv.length + ct.length];
        System.arraycopy(iv, 0, out, 0, iv.length);
        System.arraycopy(ct, 0, out, iv.length, ct.length);
        return out;
    }

    private static byte[] decrypt(byte[] blob) throws Exception {
        Cipher c = Cipher.getInstance(TRANSFORM);
        c.init(Cipher.DECRYPT_MODE, key(), new GCMParameterSpec(TAG_BITS, blob, 0, IV_LEN));
        return c.doFinal(blob, IV_LEN, blob.length - IV_LEN);
    }

    private static SecretKey key() throws Exception {
        KeyStore ks = KeyStore.getInstance("AndroidKeyStore");
        ks.load(null);
        if (ks.containsAlias(ALIAS)) {
            return (SecretKey) ks.getKey(ALIAS, null);
        }
        KeyGenerator kg = KeyGenerator.getInstance("AES", "AndroidKeyStore");
        android.security.keystore.KeyGenParameterSpec spec =
                new android.security.keystore.KeyGenParameterSpec.Builder(
                        ALIAS, android.security.keystore.KeyProperties.PURPOSE_ENCRYPT
                                | android.security.keystore.KeyProperties.PURPOSE_DECRYPT)
                        .setBlockModes(android.security.keystore.KeyProperties.BLOCK_MODE_GCM)
                        .setEncryptionPaddings(android.security.keystore.KeyProperties.ENCRYPTION_PADDING_NONE)
                        .setKeySize(256)
                        .build();
        kg.init(spec);
        return kg.generateKey();
    }

    private static SharedPreferences prefs(Context ctx) {
        return ctx.getSharedPreferences(PREF, Context.MODE_PRIVATE);
    }
}
