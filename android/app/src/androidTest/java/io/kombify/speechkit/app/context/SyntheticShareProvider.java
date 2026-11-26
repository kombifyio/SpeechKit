package io.kombify.speechkit.app.context;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.graphics.Bitmap;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;
import java.io.File;
import java.io.FileNotFoundException;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.RandomAccessFile;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;

/** Private test-APK provider; only temporary read grants expose synthetic fixtures. */
public final class SyntheticShareProvider extends ContentProvider {
    @Override public boolean onCreate() { return true; }
    @Override public String getType(Uri uri) {
        String name = uri.getLastPathSegment();
        if ("chat.png".equals(name) || "missing.png".equals(name) || "oversized.png".equals(name)) return "image/png";
        if ("silence.wav".equals(name) || "context-followup.wav".equals(name)) return "audio/wav";
        return null;
    }
    private File fixture(Uri uri) throws FileNotFoundException {
        String name = uri.getLastPathSegment();
        if (!"chat.png".equals(name) && !"silence.wav".equals(name) && !"context-followup.wav".equals(name) && !"oversized.png".equals(name))
            throw new FileNotFoundException("Synthetic fixture unavailable");
        File file = new File(getContext().getCacheDir(), name);
        if (file.exists()) return file;
        try {
            if ("chat.png".equals(name)) {
                Bitmap bitmap = Bitmap.createBitmap(600, 300, Bitmap.Config.ARGB_8888);
                try {
                    Canvas canvas = new Canvas(bitmap);
                    canvas.drawColor(Color.WHITE);
                    Paint paint = new Paint();
                    paint.setColor(Color.BLACK); paint.setTextSize(24f); paint.setAntiAlias(true);
                    canvas.drawText("Synthetic chat - no personal messages", 20f, 50f, paint);
                    canvas.drawText("Alex: Meet at the library at three.", 20f, 120f, paint);
                    canvas.drawText("Sam: I'll bring the blue book.", 20f, 180f, paint);
                    try (FileOutputStream output = new FileOutputStream(file)) {
                        if (!bitmap.compress(Bitmap.CompressFormat.PNG, 100, output)) throw new IOException("PNG encoding failed");
                    }
                } finally { bitmap.recycle(); }
            } else if ("context-followup.wav".equals(name)) {
                try (java.io.InputStream input = getContext().getAssets().open(name);
                     FileOutputStream output = new FileOutputStream(file)) {
                    byte[] buffer = new byte[8192];
                    int count;
                    while ((count = input.read(buffer)) != -1) output.write(buffer, 0, count);
                }
            } else if ("silence.wav".equals(name)) {
                int bytes = 8000;
                ByteBuffer wav = ByteBuffer.allocate(44 + bytes).order(ByteOrder.LITTLE_ENDIAN);
                wav.put("RIFF".getBytes(StandardCharsets.US_ASCII)).putInt(36 + bytes)
                    .put("WAVEfmt ".getBytes(StandardCharsets.US_ASCII)).putInt(16)
                    .putShort((short) 1).putShort((short) 1).putInt(16000).putInt(32000)
                    .putShort((short) 2).putShort((short) 16).put("data".getBytes(StandardCharsets.US_ASCII)).putInt(bytes);
                try (FileOutputStream output = new FileOutputStream(file)) { output.write(wav.array()); }
            } else {
                try (RandomAccessFile output = new RandomAccessFile(file, "rw")) { output.setLength(64L * 1024 * 1024); }
            }
        } catch (IOException error) {
            file.delete();
            FileNotFoundException failure = new FileNotFoundException("Could not create synthetic fixture");
            failure.initCause(error);
            throw failure;
        }
        return file;
    }
    @Override public ParcelFileDescriptor openFile(Uri uri, String mode) throws FileNotFoundException {
        if (!"r".equals(mode)) throw new FileNotFoundException("Read only");
        return ParcelFileDescriptor.open(fixture(uri), ParcelFileDescriptor.MODE_READ_ONLY);
    }
    @Override public Cursor query(Uri uri, String[] projection, String selection, String[] args, String order) {
        final File file;
        try { file = fixture(uri); } catch (FileNotFoundException e) { return null; }
        String[] columns = projection != null ? projection : new String[]{OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE};
        Object[] values = new Object[columns.length];
        for (int i = 0; i < columns.length; i++) {
            if (OpenableColumns.DISPLAY_NAME.equals(columns[i])) values[i] = file.getName();
            else if (OpenableColumns.SIZE.equals(columns[i])) values[i] = file.length();
        }
        MatrixCursor cursor = new MatrixCursor(columns);
        cursor.addRow(values);
        return cursor;
    }
    @Override public Uri insert(Uri uri, ContentValues values) { throw new UnsupportedOperationException(); }
    @Override public int update(Uri uri, ContentValues values, String selection, String[] args) { throw new UnsupportedOperationException(); }
    @Override public int delete(Uri uri, String selection, String[] args) { throw new UnsupportedOperationException(); }
}
