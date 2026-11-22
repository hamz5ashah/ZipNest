package com.zipnest.app;

import android.content.Context;

import org.json.JSONArray;
import org.json.JSONException;
import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.OutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Bridge to ZipNest's archive engine. The engine is a native command-line
 * program (written in Go) packaged as lib/&lt;abi&gt;/libzipnest.so; it prints one
 * JSON object per line on stdout.
 */
final class Core {
    private Core() {}

    interface Listener {
        void onProgress(String file, long done, long total);
    }

    /** A running engine process that can be cancelled. */
    static final class Job {
        volatile Process process;
        volatile boolean cancelled;

        void cancel() {
            cancelled = true;
            Process p = process;
            if (p != null) p.destroy();
        }
    }

    static final class Result {
        boolean ok;
        String code = "error";   // ok, password_required, bad_password, cancelled, error
        String message = "";
        JSONObject result;

        boolean needsPassword() {
            return "password_required".equals(code) || "bad_password".equals(code);
        }
    }

    /** One member of an archive. */
    static final class Entry {
        String path;
        boolean dir, encrypted, link;
        long size, packed, mtime;

        String name() {
            int i = path.lastIndexOf('/');
            return i < 0 ? path : path.substring(i + 1);
        }
    }

    static String binary(Context c) {
        return c.getApplicationInfo().nativeLibraryDir + "/libzipnest.so";
    }

    static Result run(Context c, List<String> args, String password, Job job, Listener listener) {
        List<String> cmd = new ArrayList<String>();
        cmd.add(binary(c));
        cmd.add("--json");
        if (password != null) cmd.add("--password-stdin");
        cmd.addAll(args);
        Result r = new Result();
        r.message = "The archive engine did not answer";
        try {
            ProcessBuilder pb = new ProcessBuilder(cmd);
            pb.environment().put("TMPDIR", c.getCacheDir().getAbsolutePath());
            final Process p = pb.start();
            job.process = p;
            OutputStream os = p.getOutputStream();
            if (password != null) os.write((password + "\n").getBytes(StandardCharsets.UTF_8));
            os.close();
            Thread drain = new Thread(new Runnable() {
                @Override public void run() {
                    try {
                        InputStream es = p.getErrorStream();
                        byte[] b = new byte[4096];
                        while (es.read(b) >= 0) { /* discard */ }
                    } catch (IOException ignored) {
                    }
                }
            });
            drain.start();
            BufferedReader br = new BufferedReader(new InputStreamReader(p.getInputStream(), StandardCharsets.UTF_8));
            String line;
            while ((line = br.readLine()) != null) {
                JSONObject o;
                try {
                    o = new JSONObject(line);
                } catch (JSONException e) {
                    continue;
                }
                String type = o.optString("type");
                if ("progress".equals(type)) {
                    if (listener != null) listener.onProgress(o.optString("file"), o.optLong("done"), o.optLong("total"));
                } else if ("result".equals(type)) {
                    r.ok = true;
                    r.code = "ok";
                    r.message = "";
                    r.result = o.optJSONObject("result");
                } else if ("error".equals(type)) {
                    r.ok = false;
                    r.code = o.optString("code", "error");
                    r.message = o.optString("message");
                }
            }
            p.waitFor();
        } catch (Exception e) {
            r.ok = false;
            r.code = "error";
            r.message = String.valueOf(e.getMessage());
        }
        if (job.cancelled) {
            r.ok = false;
            r.code = "cancelled";
            r.message = "Cancelled";
        }
        return r;
    }

    static List<Entry> parseEntries(JSONObject result) {
        List<Entry> out = new ArrayList<Entry>();
        if (result == null) return out;
        JSONArray a = result.optJSONArray("entries");
        if (a == null) return out;
        for (int i = 0; i < a.length(); i++) {
            JSONObject o = a.optJSONObject(i);
            if (o == null) continue;
            Entry e = new Entry();
            e.path = o.optString("path");
            e.dir = o.optBoolean("dir");
            e.size = o.optLong("size", -1);
            e.packed = o.optLong("packed", -1);
            e.mtime = o.optLong("mtime");
            e.encrypted = o.optBoolean("encrypted");
            e.link = o.optBoolean("link");
            out.add(e);
        }
        return out;
    }

    /** Immediate children of dir ("" = root); folders that only exist implicitly are synthesised. */
    static List<Entry> children(List<Entry> all, String dir) {
        String prefix = dir.isEmpty() ? "" : dir + "/";
        Map<String, Entry> byName = new LinkedHashMap<String, Entry>();
        for (Entry e : all) {
            if (!e.path.startsWith(prefix) || e.path.equals(dir)) continue;
            String rest = e.path.substring(prefix.length());
            int slash = rest.indexOf('/');
            if (slash >= 0) {
                String name = rest.substring(0, slash);
                Entry d = byName.get(name);
                if (d == null) {
                    d = new Entry();
                    d.path = prefix + name;
                    d.dir = true;
                    d.packed = 0;
                    byName.put(name, d);
                }
                if (!e.dir && e.size > 0) d.size += e.size;
                if (!e.dir && e.packed > 0) d.packed += e.packed;
                continue;
            }
            Entry existing = byName.get(rest);
            if (existing != null) {
                if (e.mtime != 0) existing.mtime = e.mtime;
                continue;
            }
            byName.put(rest, e);
        }
        List<Entry> out = new ArrayList<Entry>(byName.values());
        Collections.sort(out, new Comparator<Entry>() {
            @Override public int compare(Entry a, Entry b) {
                if (a.dir != b.dir) return a.dir ? -1 : 1;
                return a.name().compareToIgnoreCase(b.name());
            }
        });
        return out;
    }

    private static final String[] ARCHIVE_EXT = {
            ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tbz", ".tar.xz", ".txz", ".tar.zst", ".tzst",
            ".nest", ".zip", ".zipx", ".jar", ".apk", ".cbz", ".epub", ".7z", ".cb7", ".rar", ".cbr",
            ".tar", ".gz", ".bz2", ".xz", ".zst"
    };

    static boolean isArchive(String name) {
        return archiveExt(name) != null;
    }

    static String archiveExt(String name) {
        String n = name.toLowerCase();
        for (String e : ARCHIVE_EXT) if (n.endsWith(e)) return e;
        if (n.matches(".*\\.r[0-9][0-9]$")) return n.substring(n.length() - 4);
        return null;
    }

    /** "photos.tar.gz" -> "photos" */
    static String stem(String name) {
        String ext = archiveExt(name);
        if (ext != null) return name.substring(0, name.length() - ext.length());
        int dot = name.lastIndexOf('.');
        return dot > 0 ? name.substring(0, dot) : name;
    }
}
