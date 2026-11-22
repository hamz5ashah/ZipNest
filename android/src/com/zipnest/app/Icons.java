package com.zipnest.app;

import android.graphics.Canvas;
import android.graphics.ColorFilter;
import android.graphics.Paint;
import android.graphics.Path;
import android.graphics.PixelFormat;
import android.graphics.Rect;
import android.graphics.RectF;
import android.graphics.Typeface;
import android.graphics.drawable.Drawable;

import java.util.Locale;

/**
 * Icons drawn in code, so they look the same on every phone (emoji differ per
 * manufacturer): file-type tiles with an extension badge, and simple line
 * icons for buttons.
 */
final class Icons {
    private Icons() {}

    static final int FOLDER = 0xFFF5B400;
    static final int ARCHIVE = 0xFF6B2BE0;

    /** Colour and short label for a file name. */
    static int colorFor(String name) {
        String e = ext(name);
        if (e.matches("jpg|jpeg|png|gif|webp|bmp|heic|svg")) return 0xFF2E9E5B;
        if (e.matches("mp4|mkv|avi|mov|webm|3gp")) return 0xFFE5484D;
        if (e.matches("mp3|m4a|ogg|flac|wav|aac|opus")) return 0xFFD6409F;
        if (e.equals("pdf")) return 0xFFD93025;
        if (e.matches("doc|docx|odt|rtf")) return 0xFF2B6CD9;
        if (e.matches("xls|xlsx|ods|csv")) return 0xFF1E8E4E;
        if (e.matches("ppt|pptx|odp")) return 0xFFE46A1E;
        if (e.matches("txt|md|log|json|xml|html|ini|java|go|py|js|c|h")) return 0xFF5B6B8C;
        if (e.equals("apk")) return 0xFF3DAA5C;
        return 0xFF8A8FA3;
    }

    static String ext(String name) {
        int d = name.lastIndexOf('.');
        return d < 0 || d == name.length() - 1 ? "" : name.substring(d + 1).toLowerCase(Locale.ROOT);
    }

    /** A rounded tile: folder, archive (zipper), "up" arrow or a file with its extension. */
    static final class Tile extends Drawable {
        static final int KIND_FILE = 0, KIND_FOLDER = 1, KIND_ARCHIVE = 2, KIND_UP = 3;
        private final int kind, color, size;
        private final String label;
        private final Paint p = new Paint(Paint.ANTI_ALIAS_FLAG);

        Tile(int kind, String name, int sizePx) {
            this.kind = kind;
            this.size = sizePx;
            if (kind == KIND_FOLDER || kind == KIND_UP) color = FOLDER;
            else if (kind == KIND_ARCHIVE) color = ARCHIVE;
            else color = colorFor(name);
            String e = ext(name).toUpperCase(Locale.ROOT);
            label = e.length() > 4 ? e.substring(0, 4) : e;
        }

        @Override public void draw(Canvas c) {
            Rect b = getBounds();
            float w = b.width(), h = b.height();
            float x0 = b.left, y0 = b.top;
            p.setStyle(Paint.Style.FILL);
            // soft tinted background tile
            p.setColor((color & 0x00FFFFFF) | 0x26000000);
            c.drawRoundRect(new RectF(x0, y0, x0 + w, y0 + h), w * 0.24f, w * 0.24f, p);
            p.setColor(color);
            switch (kind) {
                case KIND_FOLDER:
                case KIND_UP: {
                    RectF tab = new RectF(x0 + w * 0.18f, y0 + h * 0.27f, x0 + w * 0.48f, y0 + h * 0.40f);
                    c.drawRoundRect(tab, w * 0.04f, w * 0.04f, p);
                    RectF body = new RectF(x0 + w * 0.18f, y0 + h * 0.34f, x0 + w * 0.82f, y0 + h * 0.74f);
                    c.drawRoundRect(body, w * 0.06f, w * 0.06f, p);
                    if (kind == KIND_UP) {
                        p.setColor(0xFFFFFFFF);
                        Path a = new Path();
                        a.moveTo(x0 + w * 0.50f, y0 + h * 0.40f);
                        a.lineTo(x0 + w * 0.63f, y0 + h * 0.55f);
                        a.lineTo(x0 + w * 0.55f, y0 + h * 0.55f);
                        a.lineTo(x0 + w * 0.55f, y0 + h * 0.67f);
                        a.lineTo(x0 + w * 0.45f, y0 + h * 0.67f);
                        a.lineTo(x0 + w * 0.45f, y0 + h * 0.55f);
                        a.lineTo(x0 + w * 0.37f, y0 + h * 0.55f);
                        a.close();
                        c.drawPath(a, p);
                    }
                    break;
                }
                case KIND_ARCHIVE: {
                    RectF body = new RectF(x0 + w * 0.24f, y0 + h * 0.20f, x0 + w * 0.76f, y0 + h * 0.80f);
                    c.drawRoundRect(body, w * 0.07f, w * 0.07f, p);
                    p.setColor(0xFFFFC23D); // zipper teeth
                    float tw = w * 0.07f, th = h * 0.06f;
                    for (int i = 0; i < 6; i++) {
                        float y = y0 + h * 0.22f + i * th;
                        float x = (i % 2 == 0) ? x0 + w * 0.5f - tw : x0 + w * 0.5f;
                        c.drawRect(x, y, x + tw, y + th * 0.9f, p);
                    }
                    RectF pull = new RectF(x0 + w * 0.42f, y0 + h * 0.58f, x0 + w * 0.58f, y0 + h * 0.70f);
                    c.drawRoundRect(pull, w * 0.03f, w * 0.03f, p);
                    break;
                }
                default: {
                    // page with folded corner
                    float l = x0 + w * 0.26f, r = x0 + w * 0.74f, t = y0 + h * 0.16f, bt = y0 + h * 0.84f, f = w * 0.14f;
                    Path page = new Path();
                    page.moveTo(l, t);
                    page.lineTo(r - f, t);
                    page.lineTo(r, t + f);
                    page.lineTo(r, bt);
                    page.lineTo(l, bt);
                    page.close();
                    c.drawPath(page, p);
                    p.setColor(0x55FFFFFF);
                    Path fold = new Path();
                    fold.moveTo(r - f, t);
                    fold.lineTo(r - f, t + f);
                    fold.lineTo(r, t + f);
                    fold.close();
                    c.drawPath(fold, p);
                    if (!label.isEmpty()) {
                        p.setColor(0xFFFFFFFF);
                        p.setTypeface(Typeface.create(Typeface.DEFAULT, Typeface.BOLD));
                        p.setTextAlign(Paint.Align.CENTER);
                        p.setTextSize(w * (label.length() > 3 ? 0.15f : 0.18f));
                        c.drawText(label, x0 + w * 0.5f, y0 + h * 0.66f, p);
                    }
                }
            }
        }

        @Override public int getIntrinsicWidth() { return size; }

        @Override public int getIntrinsicHeight() { return size; }

        @Override public void setAlpha(int a) { p.setAlpha(a); }

        @Override public void setColorFilter(ColorFilter cf) { p.setColorFilter(cf); }

        @Override public int getOpacity() { return PixelFormat.TRANSLUCENT; }
    }

    /** Simple line icons for the action buttons. */
    static final class Glyph extends Drawable {
        static final int ADD = 0, EXTRACT = 1, TEST = 2, DELETE = 3, UP = 4;
        private final int kind, size;
        private final Paint p = new Paint(Paint.ANTI_ALIAS_FLAG);

        Glyph(int kind, int color, int sizePx) {
            this.kind = kind;
            this.size = sizePx;
            p.setColor(color);
            p.setStyle(Paint.Style.STROKE);
            p.setStrokeCap(Paint.Cap.ROUND);
            p.setStrokeJoin(Paint.Join.ROUND);
        }

        @Override public void draw(Canvas c) {
            Rect b = getBounds();
            float s = Math.min(b.width(), b.height());
            float x = b.left + (b.width() - s) / 2f, y = b.top + (b.height() - s) / 2f;
            p.setStrokeWidth(s * 0.09f);
            switch (kind) {
                case ADD:
                    c.drawRoundRect(new RectF(x + s * 0.14f, y + s * 0.14f, x + s * 0.86f, y + s * 0.86f), s * 0.18f, s * 0.18f, p);
                    c.drawLine(x + s * 0.5f, y + s * 0.32f, x + s * 0.5f, y + s * 0.68f, p);
                    c.drawLine(x + s * 0.32f, y + s * 0.5f, x + s * 0.68f, y + s * 0.5f, p);
                    break;
                case EXTRACT: {
                    c.drawLine(x + s * 0.5f, y + s * 0.12f, x + s * 0.5f, y + s * 0.6f, p);
                    Path a = new Path();
                    a.moveTo(x + s * 0.32f, y + s * 0.44f);
                    a.lineTo(x + s * 0.5f, y + s * 0.62f);
                    a.lineTo(x + s * 0.68f, y + s * 0.44f);
                    c.drawPath(a, p);
                    Path tray = new Path();
                    tray.moveTo(x + s * 0.14f, y + s * 0.62f);
                    tray.lineTo(x + s * 0.14f, y + s * 0.86f);
                    tray.lineTo(x + s * 0.86f, y + s * 0.86f);
                    tray.lineTo(x + s * 0.86f, y + s * 0.62f);
                    c.drawPath(tray, p);
                    break;
                }
                case TEST: {
                    c.drawCircle(x + s * 0.5f, y + s * 0.5f, s * 0.38f, p);
                    Path ck = new Path();
                    ck.moveTo(x + s * 0.33f, y + s * 0.51f);
                    ck.lineTo(x + s * 0.45f, y + s * 0.63f);
                    ck.lineTo(x + s * 0.68f, y + s * 0.39f);
                    c.drawPath(ck, p);
                    break;
                }
                case DELETE: {
                    c.drawLine(x + s * 0.16f, y + s * 0.26f, x + s * 0.84f, y + s * 0.26f, p);
                    c.drawLine(x + s * 0.40f, y + s * 0.14f, x + s * 0.60f, y + s * 0.14f, p);
                    Path can = new Path();
                    can.moveTo(x + s * 0.26f, y + s * 0.28f);
                    can.lineTo(x + s * 0.31f, y + s * 0.86f);
                    can.lineTo(x + s * 0.69f, y + s * 0.86f);
                    can.lineTo(x + s * 0.74f, y + s * 0.28f);
                    c.drawPath(can, p);
                    c.drawLine(x + s * 0.44f, y + s * 0.42f, x + s * 0.44f, y + s * 0.72f, p);
                    c.drawLine(x + s * 0.56f, y + s * 0.42f, x + s * 0.56f, y + s * 0.72f, p);
                    break;
                }
                default: { // UP arrow
                    c.drawLine(x + s * 0.5f, y + s * 0.18f, x + s * 0.5f, y + s * 0.82f, p);
                    Path a = new Path();
                    a.moveTo(x + s * 0.26f, y + s * 0.42f);
                    a.lineTo(x + s * 0.5f, y + s * 0.18f);
                    a.lineTo(x + s * 0.74f, y + s * 0.42f);
                    c.drawPath(a, p);
                }
            }
        }

        @Override public int getIntrinsicWidth() { return size; }

        @Override public int getIntrinsicHeight() { return size; }

        @Override public void setAlpha(int a) { p.setAlpha(a); }

        @Override public void setColorFilter(ColorFilter cf) { p.setColorFilter(cf); }

        @Override public int getOpacity() { return PixelFormat.TRANSLUCENT; }
    }
}
