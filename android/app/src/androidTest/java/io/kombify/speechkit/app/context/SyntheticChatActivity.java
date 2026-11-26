package io.kombify.speechkit.app.context;

import android.app.Activity;
import android.content.ClipData;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.view.Gravity;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

/** Fictional chat running in the test APK, without a messenger account. */
public final class SyntheticChatActivity extends Activity {
    private String destination;
    private LinearLayout layout;

    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        getWindow().addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
        destination = getIntent().getStringExtra("target_package");
        if (destination == null) { finish(); return; }
        layout = new LinearLayout(this);
        layout.setOrientation(LinearLayout.VERTICAL);
        layout.setPadding(32, 32, 32, 32);
        message("Synthetic chat — no personal messages");
        message("Alex: Meet at the library at three.");
        message("Sam: I'll bring the blue book.");
        message("Synthetic image: library plan");
        message("Synthetic voice note: silence");
        EditText editor = new EditText(this);
        editor.setHint("Write a synthetic reply");
        editor.setContentDescription("Synthetic reply");
        editor.setGravity(Gravity.TOP);
        editor.setMinLines(2);
        layout.addView(editor);
        button("Share synthetic text", "text/plain", null);
        button("Share synthetic image", "image/png", "chat.png");
        button("Share synthetic silence", "audio/wav", "silence.wav");
        ScrollView scroll = new ScrollView(this);
        scroll.addView(layout);
        setContentView(scroll);
        String automatic = getIntent().getStringExtra("auto_share");
        if (automatic != null) getWindow().getDecorView().post(() -> {
            switch (automatic) {
                case "text": share("text/plain", null); break;
                case "chat.png": case "missing.png": case "oversized.png": share("image/png", automatic); break;
                case "silence.wav": case "context-followup.wav": share("audio/wav", automatic); break;
                default: finish(); return;
            }
            finish();
        });
    }

    private void message(String value) {
        TextView view = new TextView(this);
        view.setText(value);
        view.setTextSize(20f);
        view.setPadding(0, 16, 0, 16);
        layout.addView(view);
    }

    private void button(String label, String type, String fixture) {
        Button view = new Button(this);
        view.setText(label);
        view.setOnClickListener(ignored -> share(type, fixture));
        layout.addView(view);
    }

    private void share(String type, String fixture) {
        Intent share = new Intent(Intent.ACTION_SEND).setType(type)
            .setClassName(destination, "io.kombify.speechkit.app.context.ContentAnalysisActivity");
        if (fixture == null) {
            share.putExtra(Intent.EXTRA_TEXT, "Alex: Meet at the library at three.\nSam: I'll bring the blue book.");
        } else {
            Uri uri = Uri.parse("content://" + getPackageName() + ".contextfixtures/" + fixture);
            share.putExtra(Intent.EXTRA_STREAM, uri);
            share.setClipData(ClipData.newRawUri("Synthetic fixture", uri));
            share.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        }
        startActivity(share);
    }
}
