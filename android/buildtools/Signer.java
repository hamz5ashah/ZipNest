import com.android.apksig.ApkSigner;
import com.android.apksig.ApkVerifier;

import java.io.File;
import java.io.FileInputStream;
import java.security.KeyStore;
import java.security.PrivateKey;
import java.security.cert.X509Certificate;
import java.util.Collections;

/**
 * Signs (APK Signature Scheme v2) and verifies an APK with Google's apksig library.
 * usage: Signer <keystore.p12> <storepass> <alias> <in.apk> <out.apk>
 */
public class Signer {
    public static void main(String[] a) throws Exception {
        KeyStore ks = KeyStore.getInstance("PKCS12");
        try (FileInputStream in = new FileInputStream(a[0])) {
            ks.load(in, a[1].toCharArray());
        }
        PrivateKey key = (PrivateKey) ks.getKey(a[2], a[1].toCharArray());
        X509Certificate cert = (X509Certificate) ks.getCertificate(a[2]);

        ApkSigner.SignerConfig cfg = new ApkSigner.SignerConfig.Builder(
                "ZIPNEST", key, Collections.singletonList(cert)).build();
        new ApkSigner.Builder(Collections.singletonList(cfg))
                .setInputApk(new File(a[3]))
                .setOutputApk(new File(a[4]))
                .setV1SigningEnabled(false) // v1 (JAR) signing is only needed below Android 7
                .setV2SigningEnabled(true)
                .build()
                .sign();

        ApkVerifier.Result r = new ApkVerifier.Builder(new File(a[4])).build().verify();
        for (ApkVerifier.IssueWithParams e : r.getErrors()) System.err.println("ERROR: " + e);
        for (ApkVerifier.IssueWithParams w : r.getWarnings()) System.err.println("WARN: " + w);
        System.out.println("verified=" + r.isVerified() + " v1=" + r.isVerifiedUsingV1Scheme()
                + " v2=" + r.isVerifiedUsingV2Scheme());
        if (!r.isVerified()) System.exit(1);
    }
}
