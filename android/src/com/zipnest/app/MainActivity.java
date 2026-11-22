package com.zipnest.app;

import android.Manifest;
import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.ClipData;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.graphics.Color;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Environment;
import android.os.Handler;
import android.os.Looper;
import android.provider.OpenableColumns;
import android.provider.Settings;
import android.view.View;
import android.view.Window;
import android.webkit.JavascriptInterface;
import android.webkit.ValueCallback;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Toast;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;

/**
 * ZipNest for Android: a full-screen WebView showing the ZipNest interface,
 * which is served on 127.0.0.1 by the archive engine (lib/&lt;abi&gt;/libzipnest.so,
 * started with "serve"). The page talks to this activity through the
 * "ZipNestAndroid" JavaScript interface for things only Android can do.
 */
public class MainActivity extends Activity {
    private static final int REQ_STORAGE = 1;

    private WebView web;
    private Process engine;
    private String baseUrl;          // http://127.0.0.1:port
    private boolean pageReady;
    private String pendingOpen;      // file to open once the page is loaded
    private boolean hadStorage;
    private final Handler ui = new Handler(Looper.getMainLooper());

    @Override protected void onCreate(Bundle state) {
        super.onCreate(state);
        Window w = getWindow();
        setBars(false);
        web = new WebView(this);
        web.setBackgroundColor(Color.rgb(0xf4, 0xf4, 0xf7));
        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        s.setAllowFileAccess(false);
        s.setAllowContentAccess(false);
        s.setSupportZoom(false);
        s.setMediaPlaybackRequiresUserGesture(true);
        web.addJavascriptInterface(new Bridge(), "ZipNestAndroid");
        web.setWebViewClient(new WebViewClient() {
            @Override public boolean shouldOverrideUrlLoading(WebView v, WebResourceRequest r) {
                Uri u = r.getUrl();
                if (baseUrl != null && u.toString().startsWith(baseUrl)) return false;
                try { startActivity(new Intent(Intent.ACTION_VIEW, u)); } catch (ActivityNotFoundException ignored) {}
                return true;
            }

            @Override public void onPageFinished(WebView v, String url) {
                pageReady = true;
                if (pendingOpen != null) {
                    openInPage(pendingOpen);
                    pendingOpen = null;
                }
            }
        });
        setContentView(web);
        hadStorage = hasStorage();
        String start = null;
        Intent in = getIntent();
        if (in != null && Intent.ACTION_VIEW.equals(in.getAction()) && in.getData() != null) {
            receive(in.getData());
        }
        startEngine(start);
    }

    // ------------------------------------------------------------ engine

    private String places() {
        JSONArray a = new JSONArray();
        try {
            File root = Environment.getExternalStorageDirectory();
            add(a, "Internal storage", root, "sd");
            add(a, "Downloads", Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS), "downloads");
            add(a, "Documents", Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOCUMENTS), "documents");
            add(a, "Pictures", Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_PICTURES), "pictures");
            add(a, "Camera", Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DCIM), "pictures");
            add(a, "Music", Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_MUSIC), "music");
            add(a, "Movies", Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_MOVIES), "videos");
            File[] ext = getExternalFilesDirs(null);
            for (int i = 1; ext != null && i < ext.length; i++) {
                if (ext[i] == null) continue;
                String p = ext[i].getAbsolutePath();
                int cut = p.indexOf("/Android/data/");
                if (cut > 0) add(a, "SD card / USB", new File(p.substring(0, cut)), "drive");
            }
        } catch (Exception ignored) {
        }
        return a.toString();
    }

    private static void add(JSONArray a, String name, File f, String icon) throws Exception {
        if (f == null) return;
        JSONObject o = new JSONObject();
        o.put("name", name);
        o.put("path", f.getAbsolutePath());
        o.put("icon", icon);
        a.put(o);
    }

    private void startEngine(final String start) {
        final ArrayList<String> cmd = new ArrayList<String>();
        cmd.add(getApplicationInfo().nativeLibraryDir + "/libzipnest.so");
        cmd.add("serve");
        cmd.add("--platform");
        cmd.add("android");
        cmd.add("--data");
        cmd.add(getFilesDir().getAbsolutePath());
        cmd.add("--temp");
        cmd.add(new File(getCacheDir(), "zipnest").getAbsolutePath());
        cmd.add("--places");
        cmd.add(places());
        if (start != null) {
            cmd.add("--start");
            cmd.add(start);
        }
        new Thread(new Runnable() {
            @Override public void run() {
                String error = null;
                try {
                    ProcessBuilder pb = new ProcessBuilder(cmd);
                    pb.redirectErrorStream(false);
                    engine = pb.start();
                    BufferedReader r = new BufferedReader(new InputStreamReader(engine.getInputStream(), StandardCharsets.UTF_8));
                    String line = r.readLine();
                    if (line == null) throw new Exception("the archive engine did not start");
                    JSONObject o = new JSONObject(line);
                    if (o.has("error")) throw new Exception(o.getString("error"));
                    final String url = o.getString("url");
                    Uri u = Uri.parse(url);
                    baseUrl = u.getScheme() + "://" + u.getAuthority();
                    ui.post(new Runnable() {
                        @Override public void run() { web.loadUrl(url); }
                    });
                } catch (Exception e) {
                    error = e.getMessage();
                }
                if (error != null) {
                    final String msg = error;
                    ui.post(new Runnable() {
                        @Override public void run() {
                            web.loadData("<html><body style='font-family:sans-serif;padding:32px'><h2>ZipNest could not start</h2><p>"
                                    + android.text.Html.escapeHtml(msg) + "</p></body></html>", "text/html", "utf-8");
                        }
                    });
                }
            }
        }).start();
    }

    @Override protected void onDestroy() {
        super.onDestroy();
        if (engine != null) {
            try { engine.getOutputStream().close(); } catch (Exception ignored) {}
            engine.destroy();
        }
        if (web != null) web.destroy();
    }

    // ------------------------------------------------------------ files from other apps

    @Override protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        if (Intent.ACTION_VIEW.equals(intent.getAction()) && intent.getData() != null) receive(intent.getData());
    }

    /** Copies a file shared by another app into the cache (if needed) and opens it. */
    private void receive(final Uri uri) {
        if ("file".equals(uri.getScheme()) && uri.getPath() != null) {
            openInPage(uri.getPath());
            return;
        }
        final String name = displayName(uri);
        final File dest = new File(new File(getCacheDir(), "incoming"), name);
        new Thread(new Runnable() {
            @Override public void run() {
                try {
                    dest.getParentFile().mkdirs();
                    InputStream in = getContentResolver().openInputStream(uri);
                    if (in == null) throw new java.io.IOException("cannot read " + uri);
                    OutputStream out = new FileOutputStream(dest);
                    byte[] buf = new byte[256 * 1024];
                    int n;
                    while ((n = in.read(buf)) > 0) out.write(buf, 0, n);
                    out.close();
                    in.close();
                    ui.post(new Runnable() {
                        @Override public void run() { openInPage(dest.getAbsolutePath()); }
                    });
                } catch (final Exception e) {
                    ui.post(new Runnable() {
                        @Override public void run() {
                            Toast.makeText(MainActivity.this, "Cannot open file: " + e.getMessage(), Toast.LENGTH_LONG).show();
                        }
                    });
                }
            }
        }).start();
    }

    private void openInPage(String path) {
        if (!pageReady) {
            pendingOpen = path;
            return;
        }
        web.evaluateJavascript("window.znOpen && znOpen(" + JSONObject.quote(path) + ")", null);
    }

    private String displayName(Uri uri) {
        String name = null;
        try {
            Cursor c = getContentResolver().query(uri, new String[]{OpenableColumns.DISPLAY_NAME}, null, null, null);
            if (c != null) {
                if (c.moveToFirst()) name = c.getString(0);
                c.close();
            }
        } catch (Exception ignored) {
        }
        if (name == null) name = uri.getLastPathSegment();
        if (name == null) name = "archive";
        return name.replace('/', '_');
    }

    // ------------------------------------------------------------ back button, permissions

    @Override public void onBackPressed() {
        if (!pageReady) {
            super.onBackPressed();
            return;
        }
        web.evaluateJavascript("window.znBack ? znBack() : false", new ValueCallback<String>() {
            @Override public void onReceiveValue(String handled) {
                if (!"true".equals(handled)) finish();
            }
        });
    }

    @Override protected void onResume() {
        super.onResume();
        boolean now = hasStorage();
        if (now && !hadStorage && pageReady) web.evaluateJavascript("window.znStorageGranted && znStorageGranted()", null);
        hadStorage = now;
    }

    private boolean hasStorage() {
        if (Build.VERSION.SDK_INT >= 30) return Environment.isExternalStorageManager();
        return checkSelfPermission(Manifest.permission.WRITE_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED;
    }

    private void requestStorage() {
        if (Build.VERSION.SDK_INT >= 30) {
            try {
                startActivity(new Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, Uri.parse("package:" + getPackageName())));
            } catch (ActivityNotFoundException e) {
                startActivity(new Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION));
            }
        } else {
            requestPermissions(new String[]{Manifest.permission.READ_EXTERNAL_STORAGE, Manifest.permission.WRITE_EXTERNAL_STORAGE}, REQ_STORAGE);
        }
    }

    @Override public void onRequestPermissionsResult(int req, String[] perms, int[] results) {
        onResume();
    }

    /** Colours the status and navigation bars to match the page theme. */
    private void setBars(boolean dark) {
        Window w = getWindow();
        int bg = dark ? Color.rgb(0x1f, 0x1f, 0x27) : Color.WHITE;
        w.setStatusBarColor(bg);
        w.setNavigationBarColor(bg);
        int flags = 0;
        if (!dark) {
            flags |= View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR;
            if (Build.VERSION.SDK_INT >= 27) flags |= View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR;
        }
        w.getDecorView().setSystemUiVisibility(flags);
        if (web != null) web.setBackgroundColor(dark ? Color.rgb(0x17, 0x17, 0x1d) : Color.rgb(0xf4, 0xf4, 0xf7));
    }

    // ------------------------------------------------------------ JavaScript bridge

    private final class Bridge {
        @JavascriptInterface public boolean hasStorage() { return MainActivity.this.hasStorage(); }

        @JavascriptInterface public void requestStorage() {
            ui.post(new Runnable() {
                @Override public void run() { MainActivity.this.requestStorage(); }
            });
        }

        @JavascriptInterface public void setTheme(final boolean dark) {
            ui.post(new Runnable() {
                @Override public void run() { setBars(dark); }
            });
        }

        @JavascriptInterface public void openFile(final String path) {
            ui.post(new Runnable() {
                @Override public void run() {
                    File f = new File(path);
                    Intent i = new Intent(Intent.ACTION_VIEW);
                    i.setDataAndType(CacheProvider.uriFor(f), CacheProvider.mimeFor(f.getName()));
                    i.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
                    try {
                        startActivity(Intent.createChooser(i, "Open " + f.getName()));
                    } catch (ActivityNotFoundException e) {
                        Toast.makeText(MainActivity.this, "No app can open this file", Toast.LENGTH_LONG).show();
                    }
                }
            });
        }

        @JavascriptInterface public void share(final String json) {
            ui.post(new Runnable() {
                @Override public void run() {
                    try {
                        JSONArray a = new JSONArray(json);
                        ArrayList<Uri> uris = new ArrayList<Uri>();
                        for (int k = 0; k < a.length(); k++) uris.add(CacheProvider.uriFor(new File(a.getString(k))));
                        if (uris.isEmpty()) return;
                        Intent i;
                        if (uris.size() == 1) {
                            i = new Intent(Intent.ACTION_SEND);
                            i.putExtra(Intent.EXTRA_STREAM, uris.get(0));
                            i.setType(CacheProvider.mimeFor(new File(a.getString(0)).getName()));
                        } else {
                            i = new Intent(Intent.ACTION_SEND_MULTIPLE);
                            i.putParcelableArrayListExtra(Intent.EXTRA_STREAM, uris);
                            i.setType("*/*");
                        }
                        ClipData clip = ClipData.newRawUri("", uris.get(0));
                        for (int k = 1; k < uris.size(); k++) clip.addItem(new ClipData.Item(uris.get(k)));
                        i.setClipData(clip);
                        i.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
                        startActivity(Intent.createChooser(i, "Share"));
                    } catch (Exception e) {
                        Toast.makeText(MainActivity.this, "Cannot share: " + e.getMessage(), Toast.LENGTH_LONG).show();
                    }
                }
            });
        }
    }
}
