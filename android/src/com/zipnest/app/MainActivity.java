package com.zipnest.app;

import android.Manifest;
import android.app.ActionBar;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.ActivityNotFoundException;
import android.content.Context;
import android.content.DialogInterface;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.content.res.Configuration;
import android.database.Cursor;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.net.Uri;
import android.os.Build;
import android.os.Bundle;
import android.os.Environment;
import android.os.Handler;
import android.os.Looper;
import android.provider.OpenableColumns;
import android.provider.Settings;
import android.text.InputType;
import android.text.TextUtils;
import android.text.format.DateFormat;
import android.text.format.Formatter;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.Menu;
import android.view.MenuItem;
import android.view.View;
import android.view.ViewGroup;
import android.widget.AdapterView;
import android.widget.ArrayAdapter;
import android.widget.BaseAdapter;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.CompoundButton;
import android.widget.EditText;
import android.widget.HorizontalScrollView;
import android.widget.ImageButton;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ListView;
import android.widget.ProgressBar;
import android.widget.RadioButton;
import android.widget.RadioGroup;
import android.widget.Spinner;
import android.widget.TextView;
import android.widget.Toast;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.Comparator;
import java.util.Date;
import java.util.List;
import java.util.Locale;
import java.util.TreeSet;

public class MainActivity extends Activity {
    private static final int BRAND = 0xFF6B2BE0;       // purple
    private static final int BRAND_2 = 0xFFE0458A;     // pink
    private static final int REQ_STORAGE = 1;

    /** One row of the list: a file system item or an archive member. */
    static final class Item {
        String name;
        boolean dir, up, archive, encrypted;
        long size = -1, packed = -1, mtime;
        File file;          // file system mode
        String path;        // archive mode: member path
    }

    private final Handler ui = new Handler(Looper.getMainLooper());
    private final List<Item> items = new ArrayList<Item>();
    private final TreeSet<Integer> selected = new TreeSet<Integer>();
    private boolean selectMode;

    private File cwd;
    private File archive;               // open archive, or null
    private String inner = "";          // folder inside the archive
    private String format = "";
    private List<Core.Entry> entries = new ArrayList<Core.Entry>();
    private String password;            // password that worked for the open archive

    private ListView list;
    private LinearLayout crumbs;          // breadcrumb chips
    private HorizontalScrollView crumbScroll;
    private LinearLayout card;            // archive info card
    private TextView cardTitle, cardInfo;
    private RowAdapter adapter;
    private Button btnAdd, btnExtract, btnTest, btnDelete;
    private TextView emptyText;
    private Button emptyButton;
    private int sortMode;   // 0 name, 1 size, 2 date, 3 type

    // Colours (light or dark theme).
    private boolean dark;
    private int cBg, cBar, cText, cSub, cActionBg, cActionText, cSel, cIconFolder, cIconArc, cIconFile, cLabel;

    private void initPalette() {
        dark = (getResources().getConfiguration().uiMode & Configuration.UI_MODE_NIGHT_MASK)
                == Configuration.UI_MODE_NIGHT_YES;
        if (dark) {
            cBg = 0xFF15131F; cBar = 0xFF221E33; cText = 0xFFECEAF5; cSub = 0xFF9E9AB3;
            cActionBg = 0xFF1C1929; cActionText = 0xFFD9C8FF; cSel = 0xFF3A2D5C;
            cIconFolder = 0xFF3D3420; cIconArc = 0xFF35295A; cIconFile = 0xFF2A2838; cLabel = 0xFFB0ACC4;
        } else {
            cBg = Color.WHITE; cBar = 0xFFF3EEFD; cText = 0xFF1E1E2A; cSub = 0xFF77778A;
            cActionBg = 0xFFF8F5FE; cActionText = 0xFF5A23C8; cSel = 0xFFEDE4FC;
            cIconFolder = 0xFFFFF4D6; cIconArc = 0xFFF0E6FD; cIconFile = 0xFFF0F0F4; cLabel = 0xFF555566;
        }
    }

    // ------------------------------------------------------------ setup

    private int dp(float v) {
        return (int) TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v, getResources().getDisplayMetrics());
    }

    @Override protected void onCreate(Bundle state) {
        initPalette();
        if (dark) setTheme(android.R.style.Theme_Material);   // dark dialogs and menus
        super.onCreate(state);
        sortMode = getPreferences(MODE_PRIVATE).getInt("sort", 0);
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setBackgroundColor(cBg);

        LinearLayout bar = new LinearLayout(this);
        bar.setOrientation(LinearLayout.HORIZONTAL);
        bar.setGravity(Gravity.CENTER_VERTICAL);
        bar.setBackgroundColor(cBar);
        bar.setPadding(dp(4), dp(2), dp(8), dp(2));
        ImageButton up = new ImageButton(this, null, android.R.attr.borderlessButtonStyle);
        up.setImageDrawable(new Icons.Glyph(Icons.Glyph.UP, cActionText, dp(22)));
        up.setContentDescription("Up one level");
        up.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { goUp(); }
        });
        bar.addView(up, new LinearLayout.LayoutParams(dp(48), dp(48)));
        crumbScroll = new HorizontalScrollView(this);
        crumbScroll.setHorizontalScrollBarEnabled(false);
        crumbs = new LinearLayout(this);
        crumbs.setOrientation(LinearLayout.HORIZONTAL);
        crumbs.setGravity(Gravity.CENTER_VERTICAL);
        crumbScroll.addView(crumbs);
        bar.addView(crumbScroll, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        root.addView(bar, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        // Archive info card (shown while an archive is open).
        card = new LinearLayout(this);
        card.setOrientation(LinearLayout.HORIZONTAL);
        card.setGravity(Gravity.CENTER_VERTICAL);
        card.setPadding(dp(14), dp(10), dp(10), dp(10));
        GradientDrawable cardBg = new GradientDrawable(GradientDrawable.Orientation.LEFT_RIGHT,
                dark ? new int[]{0xFF2A2145, 0xFF35214A} : new int[]{0xFFF1EAFE, 0xFFFBEAF3});
        card.setBackground(cardBg);
        View cardIcon = new View(this);
        cardIcon.setBackground(new Icons.Tile(Icons.Tile.KIND_ARCHIVE, "", dp(44)));
        card.addView(cardIcon, new LinearLayout.LayoutParams(dp(44), dp(44)));
        LinearLayout cardText = new LinearLayout(this);
        cardText.setOrientation(LinearLayout.VERTICAL);
        cardText.setPadding(dp(12), 0, dp(8), 0);
        cardTitle = new TextView(this);
        cardTitle.setTextSize(16);
        cardTitle.setTypeface(null, Typeface.BOLD);
        cardTitle.setTextColor(cText);
        cardTitle.setSingleLine(true);
        cardTitle.setEllipsize(TextUtils.TruncateAt.MIDDLE);
        cardInfo = new TextView(this);
        cardInfo.setTextSize(12.5f);
        cardInfo.setTextColor(cSub);
        cardText.addView(cardTitle);
        cardText.addView(cardInfo);
        card.addView(cardText, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        Button extractAll = new Button(this);
        extractAll.setText("Extract all");
        extractAll.setAllCaps(false);
        extractAll.setTextColor(Color.WHITE);
        GradientDrawable eaBg = new GradientDrawable(GradientDrawable.Orientation.LEFT_RIGHT, new int[]{BRAND, BRAND_2});
        eaBg.setCornerRadius(dp(20));
        extractAll.setBackground(eaBg);
        extractAll.setPadding(dp(16), 0, dp(16), 0);
        extractAll.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) {
                if (archive != null) extractDialog(archive, new ArrayList<String>());
            }
        });
        card.addView(extractAll, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(40)));
        card.setVisibility(View.GONE);
        root.addView(card, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));

        list = new ListView(this);
        adapter = new RowAdapter();
        list.setAdapter(adapter);
        list.setOnItemClickListener(new AdapterView.OnItemClickListener() {
            @Override public void onItemClick(AdapterView<?> p, View v, int pos, long id) { onTap(pos); }
        });
        list.setOnItemLongClickListener(new AdapterView.OnItemLongClickListener() {
            @Override public boolean onItemLongClick(AdapterView<?> p, View v, int pos, long id) {
                if (items.get(pos).up) return false;
                selectMode = true;
                toggle(pos);
                return true;
            }
        });
        FrameLayout listBox = new FrameLayout(this);
        listBox.addView(list);
        LinearLayout empty = new LinearLayout(this);
        empty.setOrientation(LinearLayout.VERTICAL);
        empty.setGravity(Gravity.CENTER);
        empty.setPadding(dp(32), dp(32), dp(32), dp(32));
        View emptyIcon = new View(this);
        emptyIcon.setBackground(new Icons.Tile(Icons.Tile.KIND_FOLDER, "", dp(72)));
        empty.addView(emptyIcon, new LinearLayout.LayoutParams(dp(72), dp(72)));
        emptyText = new TextView(this);
        emptyText.setTextSize(16);
        emptyText.setTextColor(cSub);
        emptyText.setGravity(Gravity.CENTER);
        emptyText.setPadding(0, dp(12), 0, dp(16));
        empty.addView(emptyText);
        emptyButton = new Button(this);
        emptyButton.setText("Allow access");
        emptyButton.setAllCaps(false);
        emptyButton.setTextColor(Color.WHITE);
        GradientDrawable btnBg = new GradientDrawable(GradientDrawable.Orientation.LEFT_RIGHT, new int[]{BRAND, BRAND_2});
        btnBg.setCornerRadius(dp(24));
        emptyButton.setBackground(btnBg);
        emptyButton.setPadding(dp(28), 0, dp(28), 0);
        emptyButton.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { askStorage(); }
        });
        empty.addView(emptyButton, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, dp(48)));
        listBox.addView(empty);
        list.setEmptyView(empty);
        root.addView(listBox, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1));

        LinearLayout actions = new LinearLayout(this);
        actions.setOrientation(LinearLayout.HORIZONTAL);
        actions.setBackgroundColor(cActionBg);
        btnAdd = actionButton(actions, "Add", Icons.Glyph.ADD);
        btnExtract = actionButton(actions, "Extract", Icons.Glyph.EXTRACT);
        btnTest = actionButton(actions, "Test", Icons.Glyph.TEST);
        btnDelete = actionButton(actions, "Delete", Icons.Glyph.DELETE);
        btnAdd.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { cmdAdd(); }
        });
        btnExtract.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { cmdExtract(); }
        });
        btnTest.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { cmdTest(); }
        });
        btnDelete.setOnClickListener(new View.OnClickListener() {
            @Override public void onClick(View v) { cmdDelete(); }
        });
        root.addView(actions, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        setContentView(root);

        ActionBar ab = getActionBar();
        if (ab != null) {
            ab.setTitle("ZipNest");
            ab.setBackgroundDrawable(new GradientDrawable(GradientDrawable.Orientation.LEFT_RIGHT, new int[]{BRAND, BRAND_2}));
        }
        if (Build.VERSION.SDK_INT >= 21) getWindow().setStatusBarColor(0xFF4A1E9E);

        cwd = Environment.getExternalStorageDirectory();
        if (!handleIntent(getIntent())) {
            showDir(cwd);
            if (!hasStorage()) askStorage();
        }
    }

    private Button actionButton(LinearLayout parent, String text, int glyph) {
        Button b = new Button(this, null, android.R.attr.borderlessButtonStyle);
        b.setText(text);
        b.setAllCaps(false);
        b.setTextSize(12.5f);
        b.setTextColor(cActionText);
        b.setCompoundDrawablesWithIntrinsicBounds(null, new Icons.Glyph(glyph, cActionText, dp(24)), null, null);
        b.setCompoundDrawablePadding(dp(2));
        b.setPadding(0, dp(8), 0, dp(6));
        parent.addView(b, new LinearLayout.LayoutParams(0, dp(66), 1));
        return b;
    }

    /** Breadcrumb chips: each segment can be tapped to jump there. */
    private void setCrumbs(List<String> labels, List<Runnable> actions) {
        crumbs.removeAllViews();
        for (int i = 0; i < labels.size(); i++) {
            if (i > 0) {
                TextView sep = new TextView(this);
                sep.setText("\u203A");
                sep.setTextSize(18);
                sep.setTextColor(cSub);
                sep.setPadding(dp(2), 0, dp(2), dp(2));
                crumbs.addView(sep);
            }
            final Runnable act = actions.get(i);
            TextView chip = new TextView(this);
            chip.setText(labels.get(i));
            chip.setTextSize(14);
            chip.setSingleLine(true);
            boolean last = i == labels.size() - 1;
            chip.setTextColor(last ? cText : cActionText);
            chip.setTypeface(null, last ? Typeface.BOLD : Typeface.NORMAL);
            chip.setPadding(dp(10), dp(6), dp(10), dp(6));
            GradientDrawable bg = new GradientDrawable();
            bg.setCornerRadius(dp(14));
            bg.setColor(last ? (dark ? 0xFF3A2D5C : 0xFFE6DCFB) : 0x00000000);
            chip.setBackground(bg);
            if (act != null && !last) {
                chip.setOnClickListener(new View.OnClickListener() {
                    @Override public void onClick(View v) { act.run(); }
                });
            }
            crumbs.addView(chip);
        }
        crumbScroll.post(new Runnable() {
            @Override public void run() { crumbScroll.fullScroll(View.FOCUS_RIGHT); }
        });
    }

    private void fsCrumbs(File dir) {
        List<String> labels = new ArrayList<String>();
        List<Runnable> acts = new ArrayList<Runnable>();
        File root = Environment.getExternalStorageDirectory();
        String rootPath = root.getAbsolutePath();
        String path = dir.getAbsolutePath();
        File start;
        if (path.startsWith(rootPath)) {
            start = root;
            labels.add("Internal storage");
        } else {
            start = new File("/");
            labels.add("/");
        }
        final File s0 = start;
        acts.add(new Runnable() { @Override public void run() { showDir(s0); } });
        String rest = path.substring(Math.min(path.length(), start.getAbsolutePath().length()));
        File cur = start;
        for (String seg : rest.split("/")) {
            if (seg.isEmpty()) continue;
            cur = new File(cur, seg);
            final File target = cur;
            labels.add(seg);
            acts.add(new Runnable() { @Override public void run() { showDir(target); } });
        }
        setCrumbs(labels, acts);
    }

    private void archiveCrumbs(String dir) {
        List<String> labels = new ArrayList<String>();
        List<Runnable> acts = new ArrayList<Runnable>();
        final File parent = archive.getParentFile();
        if (parent != null && !parent.getAbsolutePath().startsWith(getCacheDir().getAbsolutePath())) {
            labels.add(parent.getName().isEmpty() ? "/" : parent.getName());
            acts.add(new Runnable() { @Override public void run() { showDir(parent); } });
        }
        labels.add(archive.getName());
        acts.add(new Runnable() { @Override public void run() { showArchiveDir(""); } });
        String acc = "";
        for (String seg : dir.split("/")) {
            if (seg.isEmpty()) continue;
            acc = acc.isEmpty() ? seg : acc + "/" + seg;
            final String target = acc;
            labels.add(seg);
            acts.add(new Runnable() { @Override public void run() { showArchiveDir(target); } });
        }
        setCrumbs(labels, acts);
    }

    private void updateCard() {
        if (archive == null) {
            card.setVisibility(View.GONE);
            return;
        }
        int files = 0, enc = 0;
        long total = 0;
        for (Core.Entry e : entries) {
            if (e.dir) continue;
            files++;
            if (e.size > 0) total += e.size;
            if (e.encrypted) enc++;
        }
        long arcSize = archive.length();
        StringBuilder sb = new StringBuilder(format.toUpperCase(Locale.ROOT));
        sb.append("  \u2022  ").append(files).append(files == 1 ? " file" : " files");
        sb.append("  \u2022  ").append(size(total));
        if (total > 0 && arcSize > 0) sb.append(" \u2192 ").append(size(arcSize)).append(" (").append(arcSize * 100 / total).append("%)");
        if (enc > 0) sb.append("  \u2022  \uD83D\uDD12 protected");
        cardTitle.setText(archive.getName());
        cardInfo.setText(sb.toString());
        card.setVisibility(View.VISIBLE);
    }

    @Override protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        handleIntent(intent);
    }

    @Override protected void onResume() {
        super.onResume();
        if (archive == null && cwd != null && hasStorage()) showDir(cwd);
    }

    // ------------------------------------------------------------ permissions

    private boolean hasStorage() {
        if (Build.VERSION.SDK_INT >= 30) return Environment.isExternalStorageManager();
        return checkSelfPermission(Manifest.permission.WRITE_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED;
    }

    private void askStorage() {
        new AlertDialog.Builder(this)
                .setTitle("Allow access to files")
                .setMessage("ZipNest needs access to your files to create and extract archives.\n\n"
                        + (Build.VERSION.SDK_INT >= 30 ? "On the next screen, turn on “Allow access to manage all files” for ZipNest." : ""))
                .setPositiveButton("Continue", new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) {
                        if (Build.VERSION.SDK_INT >= 30) {
                            try {
                                startActivity(new Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION,
                                        Uri.parse("package:" + getPackageName())));
                            } catch (ActivityNotFoundException e) {
                                startActivity(new Intent(Settings.ACTION_MANAGE_ALL_FILES_ACCESS_PERMISSION));
                            }
                        } else {
                            requestPermissions(new String[]{Manifest.permission.READ_EXTERNAL_STORAGE,
                                    Manifest.permission.WRITE_EXTERNAL_STORAGE}, REQ_STORAGE);
                        }
                    }
                })
                .setNegativeButton("Not now", null)
                .show();
    }

    @Override public void onRequestPermissionsResult(int req, String[] perms, int[] results) {
        if (req == REQ_STORAGE && hasStorage() && archive == null) showDir(cwd);
    }

    // ------------------------------------------------------------ opening from other apps

    private boolean handleIntent(Intent intent) {
        if (intent == null || !Intent.ACTION_VIEW.equals(intent.getAction()) || intent.getData() == null) return false;
        final Uri uri = intent.getData();
        if ("file".equals(uri.getScheme()) && uri.getPath() != null) {
            File f = new File(uri.getPath());
            cwd = f.getParentFile();
            openArchive(f);
            return true;
        }
        final String name = displayName(uri);
        final File dest = new File(new File(getCacheDir(), "incoming"), name);
        final Core.Job job = new Core.Job();
        final AlertDialog dlg = new AlertDialog.Builder(this).setMessage("Opening " + name + "…").setCancelable(false).show();
        new Thread(new Runnable() {
            @Override public void run() {
                String err = null;
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
                } catch (Exception e) {
                    err = e.getMessage();
                }
                final String error = err;
                ui.post(new Runnable() {
                    @Override public void run() {
                        dlg.dismiss();
                        if (error != null) {
                            alert("Cannot open file", error);
                            showDir(cwd);
                        } else {
                            openArchive(dest);
                        }
                    }
                });
            }
        }).start();
        return true;
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

    // ------------------------------------------------------------ listing

    private void showDir(File dir) {
        archive = null;
        inner = "";
        entries = new ArrayList<Core.Entry>();
        password = null;
        cwd = dir;
        items.clear();
        clearSelection();
        File parent = dir.getParentFile();
        if (parent != null && parent.listFiles() != null) {
            Item upItem = new Item();
            upItem.up = true;
            upItem.name = "..";
            items.add(upItem);
        }
        File[] files = dir.listFiles();
        if (files != null) {
            Arrays.sort(files, new Comparator<File>() {
                @Override public int compare(File a, File b) {
                    if (a.isDirectory() != b.isDirectory()) return a.isDirectory() ? -1 : 1;
                    return a.getName().compareToIgnoreCase(b.getName());
                }
            });
            for (File f : files) {
                Item it = new Item();
                it.file = f;
                it.name = f.getName();
                it.dir = f.isDirectory();
                it.size = it.dir ? -1 : f.length();
                it.mtime = f.lastModified();
                it.archive = !it.dir && Core.isArchive(it.name);
                items.add(it);
            }
        }
        sortItems();
        if (files == null && !hasStorage()) {
            emptyText.setText("ZipNest needs permission to see your files.");
            emptyButton.setVisibility(View.VISIBLE);
        } else {
            emptyText.setText(files == null ? "This folder cannot be opened." : "This folder is empty.");
            emptyButton.setVisibility(View.GONE);
        }
        fsCrumbs(dir);
        updateCard();
        setSubtitle(null);
        adapter.notifyDataSetChanged();
        list.setSelection(0);
        updateButtons();
    }

    private void openArchive(final File f) {
        runCore("Opening " + f.getName(), Arrays.asList("l", f.getAbsolutePath()), password(f), f, new Done() {
            @Override public void done(Core.Result r, String pw) {
                if (!r.ok) {
                    if (!"cancelled".equals(r.code)) alert(f.getName(), r.message);
                    return;
                }
                archive = f;
                password = pw;
                format = r.result == null ? "" : r.result.optString("format");
                entries = Core.parseEntries(r.result);
                showArchiveDir("");
            }
        });
    }

    private String password(File f) {
        return f.equals(archive) ? password : null;
    }

    private void showArchiveDir(String dir) {
        inner = dir;
        items.clear();
        clearSelection();
        Item upItem = new Item();
        upItem.up = true;
        upItem.name = "..";
        items.add(upItem);
        for (Core.Entry e : Core.children(entries, dir)) {
            Item it = new Item();
            it.path = e.path;
            it.name = e.name();
            it.dir = e.dir;
            it.size = e.dir ? -1 : e.size;
            it.packed = e.packed;
            it.mtime = e.mtime;
            it.encrypted = e.encrypted;
            items.add(it);
        }
        sortItems();
        archiveCrumbs(dir);
        updateCard();
        setSubtitle(format.toUpperCase(Locale.ROOT) + " archive");
        adapter.notifyDataSetChanged();
        list.setSelection(0);
        updateButtons();
    }

    /** Sorts the rows: ".." first, then folders, then files, by the chosen sort mode. */
    private void sortItems() {
        Collections.sort(items, new Comparator<Item>() {
            @Override public int compare(Item a, Item b) {
                if (a.up != b.up) return a.up ? -1 : 1;
                if (a.dir != b.dir) return a.dir ? -1 : 1;
                int c = 0;
                if (sortMode == 1) c = Long.compare(b.size, a.size);
                else if (sortMode == 2) c = Long.compare(b.mtime, a.mtime);
                else if (sortMode == 3) c = ext(a.name).compareToIgnoreCase(ext(b.name));
                return c != 0 ? c : a.name.compareToIgnoreCase(b.name);
            }
        });
    }

    private static String ext(String n) {
        int d = n.lastIndexOf('.');
        return d < 0 ? "" : n.substring(d + 1);
    }

    private void chooseSort() {
        new AlertDialog.Builder(this)
                .setTitle("Sort by")
                .setSingleChoiceItems(new String[]{"Name", "Size (largest first)", "Date (newest first)", "Type"},
                        sortMode, new DialogInterface.OnClickListener() {
                            @Override public void onClick(DialogInterface d, int w) {
                                sortMode = w;
                                getPreferences(MODE_PRIVATE).edit().putInt("sort", w).apply();
                                clearSelection();
                                sortItems();
                                adapter.notifyDataSetChanged();
                                updateButtons();
                                d.dismiss();
                            }
                        })
                .show();
    }

    private void setSubtitle(String s) {
        ActionBar ab = getActionBar();
        if (ab != null) ab.setSubtitle(s);
    }

    private void refresh() {
        if (archive != null) {
            final String keep = inner;
            runCore("Reading " + archive.getName(), Arrays.asList("l", archive.getAbsolutePath()), password, archive, new Done() {
                @Override public void done(Core.Result r, String pw) {
                    if (!r.ok) {
                        showDir(cwd);
                        return;
                    }
                    password = pw;
                    entries = Core.parseEntries(r.result);
                    showArchiveDir(keep);
                }
            });
        } else {
            showDir(cwd);
        }
    }

    private void goUp() {
        if (archive != null) {
            if (inner.isEmpty()) {
                File p = archive.getParentFile();
                if (p == null || p.getAbsolutePath().startsWith(getCacheDir().getAbsolutePath())) p = cwd;
                showDir(p != null ? p : Environment.getExternalStorageDirectory());
            } else {
                int i = inner.lastIndexOf('/');
                showArchiveDir(i < 0 ? "" : inner.substring(0, i));
            }
            return;
        }
        File p = cwd.getParentFile();
        if (p != null && p.listFiles() != null) showDir(p);
    }

    @Override public void onBackPressed() {
        if (selectMode) {
            clearSelection();
            adapter.notifyDataSetChanged();
            updateButtons();
            return;
        }
        if (archive != null) {
            goUp();
            return;
        }
        File p = cwd.getParentFile();
        if (p != null && p.listFiles() != null && !cwd.equals(Environment.getExternalStorageDirectory())) {
            showDir(p);
            return;
        }
        super.onBackPressed();
    }

    private void onTap(int pos) {
        Item it = items.get(pos);
        if (selectMode && !it.up) {
            toggle(pos);
            return;
        }
        if (it.up) {
            goUp();
        } else if (archive != null) {
            if (it.dir) showArchiveDir(it.path);
            else viewMember(it);
        } else if (it.dir) {
            showDir(it.file);
        } else if (it.archive) {
            openArchive(it.file);
        } else {
            openWithOtherApp(it.file);
        }
    }

    private void toggle(int pos) {
        if (selected.contains(pos)) selected.remove(pos);
        else selected.add(pos);
        if (selected.isEmpty()) selectMode = false;
        adapter.notifyDataSetChanged();
        updateButtons();
    }

    private void clearSelection() {
        selected.clear();
        selectMode = false;
    }

    private List<Item> selection() {
        List<Item> out = new ArrayList<Item>();
        for (int i : selected) if (i < items.size() && !items.get(i).up) out.add(items.get(i));
        return out;
    }

    private void updateButtons() {
        int n = selected.size();
        ActionBar ab = getActionBar();
        if (ab != null) ab.setTitle(n > 0 ? n + " selected" : (archive != null ? archive.getName() : "ZipNest"));
        btnDelete.setEnabled(n > 0);
        btnDelete.setAlpha(n > 0 ? 1f : 0.4f);
        invalidateOptionsMenu();
    }

    // ------------------------------------------------------------ commands

    private void cmdAdd() {
        if (archive != null) {
            alert("Add files", "To add files to this archive, go back to the folder view, select the files "
                    + "(long-press), tap Add and choose the same archive name.");
            return;
        }
        List<Item> sel = selection();
        if (sel.isEmpty()) {
            toast("Long-press files or folders to select them, then tap Add");
            return;
        }
        final List<File> files = new ArrayList<File>();
        for (Item it : sel) files.add(it.file);
        addDialog(files);
    }

    private static final String[] FORMATS = {"zip", "nest", "7z", "tar", "tar.gz", "tar.xz", "tar.zst"};
    private static final String[] RR_NAMES = {"No recovery record", "Recovery record 3 %", "Recovery record 5 %", "Recovery record 10 %"};
    private static final String[] RR_ARGS = {"", "-rr3", "-rr5", "-rr10"};
    private static final String[] SPLIT_NAMES = {"Don't split", "10 MB", "25 MB (e-mail)", "100 MB", "700 MB", "1 GB", "4 GB (FAT32)"};
    private static final String[] SPLIT_ARGS = {"", "-v10m", "-v25m", "-v100m", "-v700m", "-v1g", "-v4092m"};
    private static final String[] LEVEL_NAMES = {"Store", "Fastest", "Normal", "Best"};
    private static final int[] LEVELS = {0, 1, 6, 9};

    private void addDialog(final List<File> files) {
        LinearLayout v = new LinearLayout(this);
        v.setOrientation(LinearLayout.VERTICAL);
        v.setPadding(dp(20), dp(8), dp(20), 0);
        String base = files.size() == 1 ? stripExt(files.get(0)) : cwd.getName();
        if (base.isEmpty()) base = "archive";
        final EditText name = new EditText(this);
        name.setSingleLine(true);
        name.setText(base);
        v.addView(label("Archive name"));
        v.addView(name);
        final Spinner fmt = spinner(FORMATS, 0);
        v.addView(label("Format"));
        v.addView(fmt);
        final Spinner level = spinner(LEVEL_NAMES, 2);
        v.addView(label("Compression"));
        v.addView(level);
        final Spinner split = spinner(SPLIT_NAMES, 0);
        v.addView(label("Split into parts"));
        v.addView(split);
        v.addView(label("Password (ZIP and 7z, AES-256) \u2013 optional"));
        final EditText pw = new EditText(this);
        pw.setSingleLine(true);
        pw.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        v.addView(pw);
        final CheckBox show = new CheckBox(this);
        show.setText("Show password");
        show.setOnCheckedChangeListener(new CompoundButton.OnCheckedChangeListener() {
            @Override public void onCheckedChanged(CompoundButton b, boolean on) {
                pw.setInputType(InputType.TYPE_CLASS_TEXT | (on ? InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD
                        : InputType.TYPE_TEXT_VARIATION_PASSWORD));
                pw.setSelection(pw.getText().length());
            }
        });
        v.addView(show);
        final CheckBox hide = new CheckBox(this);
        hide.setText("Encrypt file names too (7z, nest)");
        v.addView(hide);
        v.addView(label("Protection against damage (nest only)"));
        final Spinner rr = spinner(RR_NAMES, 0);
        v.addView(rr);
        new AlertDialog.Builder(this)
                .setTitle("Create archive")
                .setView(v)
                .setPositiveButton("OK", new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) {
                        String f = FORMATS[fmt.getSelectedItemPosition()];
                        String n = name.getText().toString().trim();
                        if (n.isEmpty()) n = "archive";
                        if (!n.toLowerCase(Locale.ROOT).endsWith("." + f)) n += "." + f;
                        String p = pw.getText().toString();
                        if (!p.isEmpty() && !"zip".equals(f) && !"7z".equals(f) && !"nest".equals(f)) {
                            alert("Password", "Passwords are supported for ZIP, 7z and nest archives.");
                            return;
                        }
                        if (hide.isChecked() && (p.isEmpty() || !("7z".equals(f) || "nest".equals(f)))) {
                            alert("Encrypt file names", "Choose 7z or nest and enter a password to hide the file names.");
                            return;
                        }
                        if (rr.getSelectedItemPosition() > 0 && !"nest".equals(f)) {
                            alert("Recovery record", "Recovery records are available for the nest format.");
                            return;
                        }
                        final File out = new File(cwd, n);
                        List<String> args = new ArrayList<String>(Arrays.asList("a", "-m" + LEVELS[level.getSelectedItemPosition()],
                                "--format=" + f));
                        String sp = SPLIT_ARGS[split.getSelectedItemPosition()];
                        if (!sp.isEmpty()) args.add(sp);
                        if (hide.isChecked()) args.add("--encrypt-names");
                        String rrArg = RR_ARGS[rr.getSelectedItemPosition()];
                        if (!rrArg.isEmpty()) args.add(rrArg);
                        args.add(out.getAbsolutePath());
                        for (File x : files) args.add(x.getAbsolutePath());
                        runCore("Creating " + out.getName(), args, p.isEmpty() ? null : p, null, new Done() {
                            @Override public void done(Core.Result r, String pw) {
                                if (!r.ok) {
                                    if (!"cancelled".equals(r.code)) alert("Could not create archive", r.message);
                                } else {
                                    toast("Created " + out.getName());
                                }
                                showDir(cwd);
                            }
                        });
                    }
                })
                .setNegativeButton("Cancel", null)
                .show();
    }

    private static final String[] CONVERT_FORMATS = {"zip", "nest", "7z", "tar.gz", "tar.xz", "tar.zst"};

    /** Repacks an archive into another format next to the original (or in Downloads). */
    private void convertDialog(final File src) {
        LinearLayout v = new LinearLayout(this);
        v.setOrientation(LinearLayout.VERTICAL);
        v.setPadding(dp(20), dp(8), dp(20), 0);
        v.addView(label("Convert \u201C" + src.getName() + "\u201D to:"));
        final Spinner fmt = spinner(CONVERT_FORMATS, 1);
        v.addView(fmt);
        final Spinner level = spinner(LEVEL_NAMES, 2);
        v.addView(label("Compression"));
        v.addView(level);
        v.addView(label("New password (ZIP / 7z) \u2013 optional"));
        final EditText pw = new EditText(this);
        pw.setSingleLine(true);
        pw.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        v.addView(pw);
        new AlertDialog.Builder(this)
                .setTitle("Convert archive")
                .setView(v)
                .setPositiveButton("Convert", new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) {
                        String f = CONVERT_FORMATS[fmt.getSelectedItemPosition()];
                        final File out = new File(defaultExtractDir(src), Core.stem(src.getName()) + "." + f);
                        if (out.equals(src)) {
                            alert("Convert", "The archive already has this format.");
                            return;
                        }
                        List<String> args = new ArrayList<String>(Arrays.asList("c", "-m" + LEVELS[level.getSelectedItemPosition()],
                                "--format=" + f));
                        String np = pw.getText().toString();
                        if (!np.isEmpty()) args.add("--new-password=" + np);
                        args.add(src.getAbsolutePath());
                        args.add(out.getAbsolutePath());
                        runCore("Converting " + src.getName(), args, password(src), src, new Done() {
                            @Override public void done(Core.Result r, String pw) {
                                if (!r.ok) {
                                    if (!"cancelled".equals(r.code)) alert("Conversion failed", r.message);
                                    return;
                                }
                                alert("Done", src.getName() + " (" + size(src.length()) + ")\n\u2192 " + out.getName()
                                        + " (" + size(out.length()) + ")\n\nSaved in " + out.getParent());
                                if (archive == null) showDir(cwd);
                            }
                        });
                    }
                })
                .setNegativeButton("Cancel", null)
                .show();
    }

    private void cmdRepair() {
        File target = archive;
        if (target == null) {
            for (Item it : selection()) if (it.archive) { target = it.file; break; }
        }
        if (target == null || !target.getName().toLowerCase(Locale.ROOT).contains(".nest")) {
            alert("Repair", "Repair works with ZipNest .nest archives that have a recovery record.");
            return;
        }
        final File t = target;
        runCore("Repairing " + t.getName(), Arrays.asList("r", t.getAbsolutePath()), null, null, new Done() {
            @Override public void done(Core.Result r, String pw) {
                JSONObject res = r.result;
                if (!r.ok) {
                    if (!"cancelled".equals(r.code)) alert("Repair", r.message);
                } else if (res == null || res.optInt("damaged") == 0) {
                    alert("Repair", t.getName() + " is not damaged. No repair needed.");
                } else {
                    alert("Repaired", "Rebuilt " + res.optInt("repaired") + " damaged part(s).\n\nSaved as:\n" + res.optString("output"));
                }
                if (archive == null) showDir(cwd);
            }
        });
    }

    private void cmdConvert() {
        File target = archive;
        if (target == null) {
            for (Item it : selection()) if (it.archive) { target = it.file; break; }
        }
        if (target == null) {
            toast("Open or select an archive to convert it");
            return;
        }
        convertDialog(target);
    }

    private void cmdExtract() {
        if (archive != null) {
            List<String> sel = new ArrayList<String>();
            for (Item it : selection()) sel.add(it.path);
            extractDialog(archive, sel);
            return;
        }
        List<Item> sel = selection();
        List<File> arcs = new ArrayList<File>();
        for (Item it : sel) if (it.archive) arcs.add(it.file);
        if (arcs.isEmpty()) {
            toast("Long-press an archive to select it, then tap Extract");
            return;
        }
        if (arcs.size() == 1) {
            extractDialog(arcs.get(0), new ArrayList<String>());
        } else {
            extractMany(arcs, 0);
        }
    }

    /** Extracts several archives, each into a folder named after it. */
    private void extractMany(final List<File> arcs, final int i) {
        if (i >= arcs.size()) {
            toast("Extracted " + arcs.size() + " archives");
            showDir(cwd);
            return;
        }
        File a = arcs.get(i);
        File dest = new File(a.getParentFile(), Core.stem(a.getName()));
        runCore("Extracting " + a.getName(), Arrays.asList("x", a.getAbsolutePath(), dest.getAbsolutePath()), null, a, new Done() {
            @Override public void done(Core.Result r, String pw) {
                if (!r.ok) {
                    if (!"cancelled".equals(r.code)) alert(arcs.get(i).getName(), r.message);
                    showDir(cwd);
                    return;
                }
                extractMany(arcs, i + 1);
            }
        });
    }

    private File defaultExtractDir(File arc) {
        File parent = arc.getParentFile();
        if (parent == null || parent.getAbsolutePath().startsWith(getCacheDir().getAbsolutePath()) || !parent.canWrite()) {
            parent = Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS);
        }
        return parent;
    }

    private void extractDialog(final File arc, final List<String> sel) {
        final File parent = defaultExtractDir(arc);
        final File sub = new File(parent, Core.stem(arc.getName()));
        LinearLayout v = new LinearLayout(this);
        v.setOrientation(LinearLayout.VERTICAL);
        v.setPadding(dp(20), dp(8), dp(20), 0);
        v.addView(label(sel.isEmpty() ? "Extract all files to:" : "Extract " + sel.size() + " selected item(s) to:"));
        final RadioGroup g = new RadioGroup(this);
        final RadioButton rSub = new RadioButton(this);
        rSub.setText("Folder “" + sub.getName() + "”\n" + parent.getAbsolutePath());
        rSub.setId(1);
        final RadioButton rHere = new RadioButton(this);
        rHere.setText("Here\n" + parent.getAbsolutePath());
        rHere.setId(2);
        final RadioButton rCustom = new RadioButton(this);
        rCustom.setText("Another folder:");
        rCustom.setId(3);
        g.addView(rSub);
        g.addView(rHere);
        g.addView(rCustom);
        g.check(1);
        v.addView(g);
        final EditText custom = new EditText(this);
        custom.setSingleLine(true);
        custom.setText(sub.getAbsolutePath());
        custom.setOnFocusChangeListener(new View.OnFocusChangeListener() {
            @Override public void onFocusChange(View view, boolean has) {
                if (has) g.check(3);
            }
        });
        v.addView(custom);
        v.addView(label("If a file already exists"));
        final Spinner mode = spinner(new String[]{"Overwrite", "Skip", "Keep both"}, 0);
        v.addView(mode);
        new AlertDialog.Builder(this)
                .setTitle("Extract " + arc.getName())
                .setView(v)
                .setPositiveButton("Extract", new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) {
                        File dest;
                        int id = g.getCheckedRadioButtonId();
                        if (id == 2) dest = parent;
                        else if (id == 3) dest = new File(custom.getText().toString().trim());
                        else dest = sub;
                        String[] modes = {"-o+", "-o-", "-or"};
                        List<String> args = new ArrayList<String>(Arrays.asList("x", modes[mode.getSelectedItemPosition()]));
                        if (archive != null && !inner.isEmpty()) args.add("--base=" + inner);
                        args.add(arc.getAbsolutePath());
                        args.add(dest.getAbsolutePath());
                        args.addAll(sel);
                        final File finalDest = dest;
                        runCore("Extracting " + arc.getName(), args, password(arc), arc, new Done() {
                            @Override public void done(Core.Result r, String pw) {
                                if (!r.ok) {
                                    if (!"cancelled".equals(r.code)) alert("Extraction failed", r.message);
                                    return;
                                }
                                if (arc.equals(archive)) password = pw;
                                JSONObject res = r.result;
                                int files = res == null ? 0 : res.optInt("files");
                                JSONArray skipped = res == null ? null : res.optJSONArray("skipped");
                                String msg = "Extracted " + files + " file(s) to\n" + finalDest.getAbsolutePath();
                                if (skipped != null && skipped.length() > 0) {
                                    msg += "\n\nSkipped " + skipped.length() + ":";
                                    for (int i = 0; i < Math.min(10, skipped.length()); i++) msg += "\n• " + skipped.optString(i);
                                }
                                final File target = finalDest;
                                new AlertDialog.Builder(MainActivity.this)
                                        .setTitle("Done")
                                        .setMessage(msg)
                                        .setPositiveButton("Open folder", new DialogInterface.OnClickListener() {
                                            @Override public void onClick(DialogInterface dd, int ww) { showDir(target); }
                                        })
                                        .setNegativeButton("Close", null)
                                        .show();
                                if (archive == null) showDir(cwd);
                            }
                        });
                    }
                })
                .setNegativeButton("Cancel", null)
                .show();
    }

    private void cmdTest() {
        File target = archive;
        if (target == null) {
            for (Item it : selection()) if (it.archive) { target = it.file; break; }
        }
        if (target == null) {
            toast("Open or select an archive to test it");
            return;
        }
        final File t = target;
        runCore("Testing " + t.getName(), Arrays.asList("t", t.getAbsolutePath()), password(t), t, new Done() {
            @Override public void done(Core.Result r, String pw) {
                if (r.ok) {
                    if (t.equals(archive)) password = pw;
                    JSONObject res = r.result;
                    alert("✅ No errors found", t.getName() + "\n\n" + (res == null ? 0 : res.optInt("files")) + " file(s), "
                            + size(res == null ? 0 : res.optLong("bytes")) + " tested.");
                } else if (!"cancelled".equals(r.code)) {
                    alert("❌ Errors found", r.message);
                }
            }
        });
    }

    private void cmdDelete() {
        final List<Item> sel = selection();
        if (sel.isEmpty()) return;
        String what = sel.size() == 1 ? "“" + sel.get(0).name + "”" : sel.size() + " items";
        if (archive != null) {
            if (!"zip".equals(format) && !format.startsWith("tar")) {
                alert("Read-only archive", format.toUpperCase(Locale.ROOT) + " archives can only be extracted.");
                return;
            }
            new AlertDialog.Builder(this)
                    .setTitle("Delete from archive")
                    .setMessage("Permanently delete " + what + " from " + archive.getName() + "?")
                    .setPositiveButton("Delete", new DialogInterface.OnClickListener() {
                        @Override public void onClick(DialogInterface d, int w) {
                            List<String> args = new ArrayList<String>(Arrays.asList("d", archive.getAbsolutePath()));
                            for (Item it : sel) args.add(it.path);
                            runCore("Deleting", args, null, null, new Done() {
                                @Override public void done(Core.Result r, String pw) {
                                    if (!r.ok && !"cancelled".equals(r.code)) alert("Delete failed", r.message);
                                    refresh();
                                }
                            });
                        }
                    })
                    .setNegativeButton("Cancel", null)
                    .show();
            return;
        }
        new AlertDialog.Builder(this)
                .setTitle("Delete")
                .setMessage("Permanently delete " + what + "?")
                .setPositiveButton("Delete", new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) {
                        int failed = 0;
                        for (Item it : sel) if (!deleteTree(it.file)) failed++;
                        if (failed > 0) toast(failed + " item(s) could not be deleted");
                        showDir(cwd);
                    }
                })
                .setNegativeButton("Cancel", null)
                .show();
    }

    private static boolean deleteTree(File f) {
        File[] kids = f.isDirectory() ? f.listFiles() : null;
        if (kids != null) for (File k : kids) deleteTree(k);
        return f.delete();
    }

    private void viewMember(final Item it) {
        final File dir = new File(new File(getCacheDir(), "view"), String.valueOf(System.nanoTime()));
        String base = "";
        int slash = it.path.lastIndexOf('/');
        if (slash >= 0) base = it.path.substring(0, slash);
        List<String> args = new ArrayList<String>(Arrays.asList("x"));
        if (!base.isEmpty()) args.add("--base=" + base);
        args.add(archive.getAbsolutePath());
        args.add(dir.getAbsolutePath());
        args.add(it.path);
        runCore("Opening " + it.name, args, password, archive, new Done() {
            @Override public void done(Core.Result r, String pw) {
                if (!r.ok) {
                    if (!"cancelled".equals(r.code)) alert(it.name, r.message);
                    return;
                }
                password = pw;
                File f = new File(dir, it.name);
                if (Core.isArchive(f.getName())) openArchive(f); // archive inside an archive
                else openWithOtherApp(f);
            }
        });
    }

    private void openWithOtherApp(File f) {
        Intent i = new Intent(Intent.ACTION_VIEW);
        i.setDataAndType(CacheProvider.uriFor(f), CacheProvider.mimeFor(f.getName()));
        i.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        try {
            startActivity(Intent.createChooser(i, f.getName()));
        } catch (ActivityNotFoundException e) {
            toast("No app can open this file");
        }
    }

    // ------------------------------------------------------------ menu

    @Override public boolean onCreateOptionsMenu(Menu menu) {
        menu.add(0, 1, 0, "Select all");
        menu.add(0, 2, 0, "Storage");
        menu.add(0, 3, 0, "New folder");
        menu.add(0, 4, 0, "Archive information");
        menu.add(0, 6, 0, "Sort by\u2026");
        menu.add(0, 7, 0, "Convert archive\u2026");
        menu.add(0, 8, 0, "Repair archive");
        menu.add(0, 5, 0, "About");
        return true;
    }

    @Override public boolean onPrepareOptionsMenu(Menu menu) {
        menu.findItem(3).setVisible(archive == null);
        menu.findItem(4).setVisible(archive != null);
        return true;
    }

    @Override public boolean onOptionsItemSelected(MenuItem item) {
        switch (item.getItemId()) {
            case 1:
                selected.clear();
                for (int i = 0; i < items.size(); i++) if (!items.get(i).up) selected.add(i);
                selectMode = !selected.isEmpty();
                adapter.notifyDataSetChanged();
                updateButtons();
                return true;
            case 2:
                chooseStorage();
                return true;
            case 3:
                newFolder();
                return true;
            case 4:
                archiveInfo();
                return true;
            case 6:
                chooseSort();
                return true;
            case 7:
                cmdConvert();
                return true;
            case 8:
                cmdRepair();
                return true;
            case 5:
                alert("ZipNest 1.5", "A free archive manager.\n\nOpen & extract: ZIP (incl. encrypted), RAR (RAR4/RAR5, "
                        + "encrypted, multi-volume), 7z, TAR, GZ, BZ2, XZ, ZST and split archives (.001).\nCreate: ZIP and 7z "
                        + "(optional AES-256 password, 7z can hide file names), TAR, TAR.GZ, TAR.XZ, TAR.ZST; split into parts; convert.\n\nRAR is a format of RARLAB; ZipNest only reads it.");
                return true;
        }
        return super.onOptionsItemSelected(item);
    }

    private void chooseStorage() {
        final List<File> roots = new ArrayList<File>();
        final List<String> names = new ArrayList<String>();
        roots.add(Environment.getExternalStorageDirectory());
        names.add("Internal storage");
        roots.add(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS));
        names.add("Downloads");
        roots.add(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOCUMENTS));
        names.add("Documents");
        File[] ext = getExternalFilesDirs(null);
        for (int i = 1; ext != null && i < ext.length; i++) {
            if (ext[i] == null) continue;
            String p = ext[i].getAbsolutePath();
            int cut = p.indexOf("/Android/data/");
            if (cut > 0) {
                roots.add(new File(p.substring(0, cut)));
                names.add("SD card / USB (" + new File(p.substring(0, cut)).getName() + ")");
            }
        }
        new AlertDialog.Builder(this)
                .setTitle("Storage")
                .setItems(names.toArray(new String[0]), new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) { showDir(roots.get(w)); }
                })
                .show();
    }

    private void newFolder() {
        final EditText name = new EditText(this);
        name.setSingleLine(true);
        name.setHint("Folder name");
        LinearLayout v = new LinearLayout(this);
        v.setPadding(dp(20), dp(8), dp(20), 0);
        v.addView(name, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        new AlertDialog.Builder(this)
                .setTitle("New folder")
                .setView(v)
                .setPositiveButton("Create", new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) {
                        String n = name.getText().toString().trim();
                        if (!n.isEmpty() && !new File(cwd, n).mkdirs()) toast("Could not create folder");
                        showDir(cwd);
                    }
                })
                .setNegativeButton("Cancel", null)
                .show();
    }

    private void archiveInfo() {
        if (archive == null) return;
        int files = 0, dirs = 0, enc = 0;
        long total = 0;
        for (Core.Entry e : entries) {
            if (e.dir) {
                dirs++;
                continue;
            }
            files++;
            if (e.size > 0) total += e.size;
            if (e.encrypted) enc++;
        }
        long arcSize = archive.length();
        String ratio = total > 0 ? (arcSize * 100 / total) + "%" : "-";
        alert(archive.getName(), "Format: " + format.toUpperCase(Locale.ROOT)
                + "\nFiles: " + files + "\nFolders: " + dirs
                + "\nTotal size: " + size(total) + "\nArchive size: " + size(arcSize)
                + "\nRatio: " + ratio + "\nEncrypted files: " + enc
                + "\n\nLocation: " + archive.getParent());
    }

    // ------------------------------------------------------------ running the engine

    interface Done {
        void done(Core.Result r, String password);
    }

    /**
     * Runs the archive engine in the background with a progress dialog. When the
     * engine reports that a password is needed (or wrong), the user is asked and
     * the command is retried.
     */
    private void runCore(final String title, final List<String> args, final String pw, final File forArchive, final Done done) {
        LinearLayout v = new LinearLayout(this);
        v.setOrientation(LinearLayout.VERTICAL);
        v.setPadding(dp(24), dp(12), dp(24), dp(4));
        final TextView file = new TextView(this);
        file.setSingleLine(true);
        file.setEllipsize(TextUtils.TruncateAt.MIDDLE);
        file.setText("Please wait…");
        v.addView(file);
        final ProgressBar bar = new ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal);
        bar.setMax(1000);
        bar.setIndeterminate(true);
        v.addView(bar, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        final TextView pct = new TextView(this);
        pct.setGravity(Gravity.END);
        v.addView(pct);
        final Core.Job job = new Core.Job();
        final AlertDialog dlg = new AlertDialog.Builder(this)
                .setTitle(title)
                .setView(v)
                .setCancelable(false)
                .setNegativeButton("Cancel", new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) { job.cancel(); }
                })
                .create();
        // Show the dialog only if the work takes a moment, to avoid flicker.
        final Runnable showLater = new Runnable() {
            @Override public void run() {
                if (!isFinishing()) dlg.show();
            }
        };
        ui.postDelayed(showLater, 250);
        final long[] last = {0};
        new Thread(new Runnable() {
            @Override public void run() {
                final Core.Result r = Core.run(MainActivity.this, args, pw, job, new Core.Listener() {
                    @Override public void onProgress(final String f, final long d, final long t) {
                        long now = System.currentTimeMillis();
                        if (now - last[0] < 100) return;
                        last[0] = now;
                        ui.post(new Runnable() {
                            @Override public void run() {
                                file.setText(f);
                                if (t > 0) {
                                    bar.setIndeterminate(false);
                                    bar.setProgress((int) (d * 1000 / t));
                                    pct.setText((d * 100 / t) + "%  •  " + size(d) + " / " + size(t));
                                } else {
                                    pct.setText(size(d));
                                }
                            }
                        });
                    }
                });
                ui.post(new Runnable() {
                    @Override public void run() {
                        ui.removeCallbacks(showLater);
                        if (dlg.isShowing()) dlg.dismiss();
                        if (r.needsPassword()) {
                            askPassword(forArchive, pw != null && "bad_password".equals(r.code), new PasswordCallback() {
                                @Override public void onPassword(String np) {
                                    if (np == null) {
                                        Core.Result c = new Core.Result();
                                        c.code = "cancelled";
                                        done.done(c, pw);
                                    } else {
                                        runCore(title, args, np, forArchive, done);
                                    }
                                }
                            });
                            return;
                        }
                        done.done(r, pw);
                    }
                });
            }
        }).start();
    }

    interface PasswordCallback {
        void onPassword(String password);
    }

    private void askPassword(File arc, boolean wrong, final PasswordCallback cb) {
        LinearLayout v = new LinearLayout(this);
        v.setOrientation(LinearLayout.VERTICAL);
        v.setPadding(dp(20), dp(8), dp(20), 0);
        if (wrong) {
            TextView t = label("The password is incorrect. Please try again.");
            t.setTextColor(0xFFC62828);
            v.addView(t);
        }
        final EditText pw = new EditText(this);
        pw.setSingleLine(true);
        pw.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
        pw.setHint("Password");
        v.addView(pw);
        final CheckBox show = new CheckBox(this);
        show.setText("Show password");
        show.setOnCheckedChangeListener(new CompoundButton.OnCheckedChangeListener() {
            @Override public void onCheckedChanged(CompoundButton b, boolean on) {
                pw.setInputType(InputType.TYPE_CLASS_TEXT | (on ? InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD
                        : InputType.TYPE_TEXT_VARIATION_PASSWORD));
                pw.setSelection(pw.getText().length());
            }
        });
        v.addView(show);
        new AlertDialog.Builder(this)
                .setTitle("Enter password" + (arc != null ? " for " + arc.getName() : ""))
                .setView(v)
                .setCancelable(false)
                .setPositiveButton("OK", new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) { cb.onPassword(pw.getText().toString()); }
                })
                .setNegativeButton("Cancel", new DialogInterface.OnClickListener() {
                    @Override public void onClick(DialogInterface d, int w) { cb.onPassword(null); }
                })
                .show();
    }

    // ------------------------------------------------------------ helpers

    private TextView label(String s) {
        TextView t = new TextView(this);
        t.setText(s);
        t.setTextSize(13);
        t.setTextColor(cLabel);
        t.setPadding(0, dp(10), 0, dp(2));
        return t;
    }

    private Spinner spinner(String[] values, int sel) {
        Spinner s = new Spinner(this);
        ArrayAdapter<String> a = new ArrayAdapter<String>(this, android.R.layout.simple_spinner_item, values);
        a.setDropDownViewResource(android.R.layout.simple_spinner_dropdown_item);
        s.setAdapter(a);
        s.setSelection(sel);
        return s;
    }

    private static String stripExt(File f) {
        String n = f.getName();
        if (f.isDirectory()) return n;
        int dot = n.lastIndexOf('.');
        return dot > 0 ? n.substring(0, dot) : n;
    }

    private String size(long n) {
        return n < 0 ? "?" : Formatter.formatShortFileSize(this, n);
    }

    private void toast(String s) {
        Toast.makeText(this, s, Toast.LENGTH_LONG).show();
    }

    private void alert(String title, String msg) {
        if (isFinishing()) return;
        new AlertDialog.Builder(this).setTitle(title).setMessage(msg).setPositiveButton("OK", null).show();
    }

    private static String iconFor(Item it) {
        if (it.up) return "⬆️";
        if (it.dir) return "📁";
        if (it.archive || Core.isArchive(it.name)) return "📦";
        String n = it.name.toLowerCase(Locale.ROOT);
        if (n.matches(".*\\.(jpg|jpeg|png|gif|webp|bmp|heic)$")) return "🖼️";
        if (n.matches(".*\\.(mp4|mkv|avi|mov|webm|3gp)$")) return "🎬";
        if (n.matches(".*\\.(mp3|m4a|ogg|flac|wav|aac|opus)$")) return "🎵";
        if (n.endsWith(".pdf")) return "📕";
        if (n.matches(".*\\.(txt|md|log|csv|json|xml|html|ini)$")) return "📝";
        return "📄";
    }

    private final class RowAdapter extends BaseAdapter {
        @Override public int getCount() { return items.size(); }

        @Override public Object getItem(int i) { return items.get(i); }

        @Override public long getItemId(int i) { return i; }

        @Override public View getView(int pos, View convert, ViewGroup parent) {
            Context c = MainActivity.this;
            LinearLayout row;
            TextView icon, name, info;
            CheckBox check;
            if (convert == null) {
                row = new LinearLayout(c);
                row.setOrientation(LinearLayout.HORIZONTAL);
                row.setGravity(Gravity.CENTER_VERTICAL);
                row.setPadding(dp(12), dp(8), dp(8), dp(8));
                row.setMinimumHeight(dp(60));
                icon = new TextView(c);
                icon.setTextSize(26);
                icon.setGravity(Gravity.CENTER);
                row.addView(icon, new LinearLayout.LayoutParams(dp(44), dp(44)));
                LinearLayout texts = new LinearLayout(c);
                texts.setOrientation(LinearLayout.VERTICAL);
                texts.setPadding(dp(12), 0, dp(4), 0);
                name = new TextView(c);
                name.setTextSize(16);
                name.setTextColor(cText);
                name.setSingleLine(true);
                name.setEllipsize(TextUtils.TruncateAt.MIDDLE);
                info = new TextView(c);
                info.setTextSize(12.5f);
                info.setTextColor(cSub);
                info.setSingleLine(true);
                texts.addView(name);
                texts.addView(info);
                row.addView(texts, new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
                check = new CheckBox(c);
                check.setClickable(false);
                check.setFocusable(false);
                row.addView(check);
                row.setTag(new View[]{icon, name, info, check});
            } else {
                row = (LinearLayout) convert;
                View[] h = (View[]) row.getTag();
                icon = (TextView) h[0];
                name = (TextView) h[1];
                info = (TextView) h[2];
                check = (CheckBox) h[3];
            }
            Item it = items.get(pos);
            icon.setText("");
            int kind = it.up ? Icons.Tile.KIND_UP : it.dir ? Icons.Tile.KIND_FOLDER
                    : (it.archive || Core.isArchive(it.name)) ? Icons.Tile.KIND_ARCHIVE : Icons.Tile.KIND_FILE;
            icon.setBackground(new Icons.Tile(kind, it.name, dp(44)));
            name.setText(it.up ? "Up one level" : it.name + (it.encrypted ? "  🔒" : ""));
            name.setTypeface(null, it.dir ? Typeface.BOLD : Typeface.NORMAL);
            StringBuilder s = new StringBuilder();
            if (!it.up) {
                if (!it.dir && it.size >= 0) s.append(size(it.size));
                if (archive != null && !it.dir && it.packed >= 0) s.append("  →  ").append(size(it.packed));
                if (it.mtime > 0) {
                    if (s.length() > 0) s.append("   •   ");
                    Date d = new Date(it.mtime);
                    s.append(DateFormat.getMediumDateFormat(c).format(d)).append(' ').append(DateFormat.getTimeFormat(c).format(d));
                }
            }
            info.setText(s.toString());
            info.setVisibility(s.length() > 0 ? View.VISIBLE : View.GONE);
            check.setVisibility(selectMode && !it.up ? View.VISIBLE : View.GONE);
            check.setChecked(selected.contains(pos));
            row.setBackgroundColor(selected.contains(pos) ? cSel : Color.TRANSPARENT);
            return row;
        }
    }
}
