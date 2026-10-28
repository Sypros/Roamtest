package be.roamtest;

import android.Manifest;
import android.app.Activity;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.location.LocationManager;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputType;
import android.view.Gravity;
import android.view.View;
import android.view.WindowManager;
import android.view.inputmethod.InputMethodManager;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

import java.io.File;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

public class MainActivity extends Activity {

    private static final int DISCOVERY_PORT = 5006;
    private static final int GREEN = Color.parseColor("#1F9D55");
    private static final int AMBER = Color.parseColor("#D98B00");
    private static final int RED = Color.parseColor("#D64545");
    private static final int GREY = Color.parseColor("#3A4048");
    private static final int BG = Color.parseColor("#121417");
    private static final int CARD = Color.parseColor("#1B1E23");
    private static final int INK = Color.parseColor("#E6E8EB");
    private static final int MUTE = Color.parseColor("#9AA3AD");

    private final Handler ui = new Handler(Looper.getMainLooper());
    private SharedPreferences prefs;

    private EditText serverField, labelField;
    private TextView serverInfo, bigState, apLine, statsLine, timeLine, statusLine;
    private Button startBtn, markBtn, findBtn, pendingBtn;
    private View livePanel;
    private boolean finding;

    @Override
    protected void onCreate(Bundle saved) {
        super.onCreate(saved);
        prefs = getSharedPreferences("roamtest", MODE_PRIVATE);
        getWindow().setStatusBarColor(BG);
        getWindow().setNavigationBarColor(BG);
        setContentView(buildUi());
        serverField.setText(prefs.getString("server", ""));
        labelField.setText(prefs.getString("label", ""));
        askPermissions();
        if (serverField.getText().length() == 0) findLaptop();
    }

    @Override
    protected void onResume() {
        super.onResume();
        ui.post(refresh);
    }

    @Override
    protected void onPause() {
        super.onPause();
        ui.removeCallbacks(refresh);
    }

    // ------------------------------------------------------------------ UI

    private View buildUi() {
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(20), dp(24), dp(20), dp(24));
        root.setBackgroundColor(BG);

        TextView title = text("RoamTest", 26, INK);
        title.setTypeface(Typeface.DEFAULT_BOLD);
        root.addView(title);
        root.addView(text("VoIP roaming walk test", 14, MUTE));
        root.addView(space(16));

        // laptop
        root.addView(text("Laptop", 13, MUTE));
        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        serverField = field("found automatically");
        serverField.setInputType(InputType.TYPE_CLASS_PHONE);
        row.addView(serverField, new LinearLayout.LayoutParams(0, dp(52), 1));
        findBtn = button("Find", GREY);
        findBtn.setOnClickListener(v -> findLaptop());
        LinearLayout.LayoutParams fp = new LinearLayout.LayoutParams(dp(90), dp(52));
        fp.leftMargin = dp(8);
        row.addView(findBtn, fp);
        root.addView(row);
        serverInfo = text("", 12, MUTE);
        root.addView(serverInfo);
        root.addView(space(12));

        // label
        root.addView(text("Run name (what you changed)", 13, MUTE));
        labelField = field("e.g. baseline, ai-roaming-off");
        labelField.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        root.addView(labelField, new LinearLayout.LayoutParams(-1, dp(52)));
        root.addView(space(16));

        startBtn = button("START", GREEN);
        startBtn.setTextSize(22);
        startBtn.setOnClickListener(v -> toggle());
        root.addView(startBtn, new LinearLayout.LayoutParams(-1, dp(72)));
        root.addView(space(12));

        // live panel
        LinearLayout live = new LinearLayout(this);
        live.setOrientation(LinearLayout.VERTICAL);
        live.setPadding(dp(16), dp(14), dp(16), dp(14));
        live.setBackground(round(CARD, 12));
        bigState = text("Ready", 30, INK);
        bigState.setTypeface(Typeface.DEFAULT_BOLD);
        live.addView(bigState);
        apLine = text("", 15, INK);
        live.addView(apLine);
        statsLine = text("", 15, INK);
        live.addView(statsLine);
        timeLine = text("", 13, MUTE);
        live.addView(timeLine);
        livePanel = live;
        root.addView(live);
        root.addView(space(12));

        markBtn = button("MARK SPOT", AMBER);
        markBtn.setTextSize(20);
        markBtn.setOnClickListener(v -> {
            startService(new Intent(this, TestService.class).setAction(TestService.ACTION_MARK));
            markBtn.setText("MARKED SPOT " + (TestState.marks + 1));
            ui.postDelayed(() -> markBtn.setText("MARK SPOT"), 1200);
        });
        root.addView(markBtn, new LinearLayout.LayoutParams(-1, dp(64)));
        root.addView(space(12));

        statusLine = text("", 14, INK);
        root.addView(statusLine);
        root.addView(space(8));

        pendingBtn = button("Upload saved runs", GREY);
        pendingBtn.setOnClickListener(v -> uploadPending());
        root.addView(pendingBtn, new LinearLayout.LayoutParams(-1, dp(48)));

        ScrollView sv = new ScrollView(this);
        sv.setBackgroundColor(BG);
        sv.setFillViewport(true);
        sv.addView(root);
        return sv;
    }

    private final Runnable refresh = new Runnable() {
        @Override
        public void run() {
            boolean on = TestState.running;
            startBtn.setText(on ? "STOP" : "START");
            startBtn.setBackground(round(on ? RED : GREEN, 10));
            markBtn.setEnabled(on);
            markBtn.setAlpha(on ? 1f : 0.35f);
            labelField.setEnabled(!on);
            serverField.setEnabled(!on);
            findBtn.setEnabled(!on);

            if (on) {
                double g = TestState.gap;
                String state;
                int col;
                if (TestState.noReplies) {
                    state = "NO REPLIES";
                    col = RED;
                } else if (TestState.seconds == 0) {
                    state = "Starting...";
                    col = GREY;
                } else if (g > 1000 || TestState.loss > 50) {
                    // calibrated on real walks: roams cost 150-300 ms and are fine;
                    // gaps over a second are what callers notice
                    state = "DROPOUT";
                    col = RED;
                } else if (g > 500 || TestState.loss > 20) {
                    state = "GLITCH";
                    col = AMBER;
                } else {
                    state = "CLEAN";
                    col = GREEN;
                }
                bigState.setText(state);
                livePanel.setBackground(round(blend(col), 12));
                String ap = "?".equals(TestState.bssid) ? "not on Wi-Fi / no location permission"
                        : TestState.bssid + "  ·  " + TestState.band(TestState.freq) + "  ·  " + TestState.rssi + " dBm";
                apLine.setText(ap);
                statsLine.setText(String.format(Locale.US, "gap %.0f ms   loss %.0f%%   rtt %.0f ms",
                        g, TestState.loss, TestState.rtt));
                int s = TestState.seconds;
                timeLine.setText(String.format(Locale.US, "%d:%02d   ·   %d roams   ·   %d spots   ·   %d bad seconds",
                        s / 60, s % 60, TestState.roams, TestState.marks, TestState.badSeconds));
                if (TestState.noReplies) {
                    timeLine.append("\nLaptop not answering: check the IP, the server window and the firewall.");
                }
            } else {
                bigState.setText("Ready");
                livePanel.setBackground(round(CARD, 12));
                apLine.setText("Walk the same route every time.");
                statsLine.setText("Change one setting per run.");
                timeLine.setText("");
            }
            statusLine.setText(TestState.status);
            int n = pendingCount();
            pendingBtn.setVisibility(n > 0 && !on ? View.VISIBLE : View.GONE);
            pendingBtn.setText("Upload " + n + " saved run" + (n == 1 ? "" : "s"));

            if (on) {
                getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
            } else {
                getWindow().clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON);
            }
            ui.postDelayed(this, 250);
        }
    };

    // ------------------------------------------------------------- actions

    private void toggle() {
        if (TestState.running) {
            startService(new Intent(this, TestService.class).setAction(TestService.ACTION_STOP));
            TestState.status = "Stopping and uploading...";
            return;
        }
        String server = serverField.getText().toString().trim();
        String label = labelField.getText().toString().trim();
        if (server.isEmpty()) {
            TestState.status = "No laptop yet. Start the RoamTest server on the laptop and tap Find.";
            return;
        }
        if (label.isEmpty()) label = "run";
        if (!hasLocation()) {
            askPermissions();
            TestState.status = "Allow location: Android only shows the access point with it.";
            return;
        }
        if (!locationOn()) {
            TestState.status = "Turn on Location in quick settings: Android hides the access point without it.";
            return;
        }
        prefs.edit().putString("server", server).putString("label", label).apply();
        hideKeyboard();
        Intent i = new Intent(this, TestService.class)
                .setAction(TestService.ACTION_START)
                .putExtra(TestService.EXTRA_LABEL, label)
                .putExtra(TestService.EXTRA_SERVER, server);
        startForegroundService(i);
    }

    /** Broadcasts "ROAMTEST?" on the LAN; the laptop server answers. */
    private void findLaptop() {
        if (finding) return;
        finding = true;
        serverInfo.setText("Looking for the laptop...");
        new Thread(() -> {
            String found = null, name = "";
            try (DatagramSocket s = new DatagramSocket()) {
                s.setBroadcast(true);
                s.setSoTimeout(800);
                byte[] q = "ROAMTEST?".getBytes(StandardCharsets.US_ASCII);
                byte[] buf = new byte[256];
                for (int attempt = 0; attempt < 4 && found == null; attempt++) {
                    s.send(new DatagramPacket(q, q.length,
                            InetAddress.getByName("255.255.255.255"), DISCOVERY_PORT));
                    try {
                        DatagramPacket p = new DatagramPacket(buf, buf.length);
                        s.receive(p);
                        String reply = new String(p.getData(), 0, p.getLength(), StandardCharsets.UTF_8);
                        if (reply.startsWith("ROAMTEST")) {
                            found = p.getAddress().getHostAddress();
                            name = reply.substring(8).trim();
                        }
                    } catch (Exception ignored) {
                        // timeout, try again
                    }
                }
            } catch (Exception ignored) {
                // no network
            }
            final String ip = found, host = name;
            ui.post(() -> {
                finding = false;
                if (ip != null) {
                    serverField.setText(ip);
                    prefs.edit().putString("server", ip).apply();
                    serverInfo.setText("Found " + (host.isEmpty() ? "laptop" : host) + " at " + ip);
                } else {
                    serverInfo.setText("Not found. Is the server running and the laptop on the same network? You can also type its IP.");
                }
            });
        }).start();
    }

    private void uploadPending() {
        String server = serverField.getText().toString().trim();
        if (server.isEmpty()) {
            TestState.status = "Find the laptop first.";
            return;
        }
        TestState.status = "Uploading...";
        new Thread(() -> {
            File[] files = TestService.pendingDir(this).listFiles();
            int ok = 0, fail = 0;
            if (files != null) {
                for (File f : files) {
                    if (TestService.upload(server, f)) ok++;
                    else fail++;
                }
            }
            TestState.status = "Uploaded " + ok + (fail > 0 ? ", failed " + fail : "") + ".";
        }).start();
    }

    private int pendingCount() {
        File[] f = TestService.pendingDir(this).listFiles();
        return f == null ? 0 : f.length;
    }

    // --------------------------------------------------------- permissions

    private void askPermissions() {
        List<String> need = new ArrayList<>();
        if (!hasLocation()) need.add(Manifest.permission.ACCESS_FINE_LOCATION);
        if (Build.VERSION.SDK_INT >= 33
                && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            need.add(Manifest.permission.POST_NOTIFICATIONS);
        }
        if (!need.isEmpty()) requestPermissions(need.toArray(new String[0]), 1);
    }

    private boolean hasLocation() {
        return checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED;
    }

    private boolean locationOn() {
        LocationManager lm = (LocationManager) getSystemService(Context.LOCATION_SERVICE);
        return Build.VERSION.SDK_INT < 28 || lm.isLocationEnabled();
    }

    // ------------------------------------------------------------- helpers

    private void hideKeyboard() {
        InputMethodManager imm = (InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE);
        imm.hideSoftInputFromWindow(labelField.getWindowToken(), 0);
    }

    private TextView text(String s, int sp, int color) {
        TextView t = new TextView(this);
        t.setText(s);
        t.setTextSize(sp);
        t.setTextColor(color);
        return t;
    }

    private EditText field(String hint) {
        EditText e = new EditText(this);
        e.setHint(hint);
        e.setHintTextColor(MUTE);
        e.setTextColor(INK);
        e.setTextSize(17);
        e.setSingleLine(true);
        e.setPadding(dp(12), 0, dp(12), 0);
        e.setBackground(round(CARD, 8));
        return e;
    }

    private Button button(String s, int color) {
        Button b = new Button(this);
        b.setText(s);
        b.setTextColor(Color.WHITE);
        b.setTypeface(Typeface.DEFAULT_BOLD);
        b.setAllCaps(false);
        b.setGravity(Gravity.CENTER);
        b.setBackground(round(color, 10));
        b.setStateListAnimator(null);
        return b;
    }

    private View space(int h) {
        View v = new View(this);
        v.setLayoutParams(new LinearLayout.LayoutParams(1, dp(h)));
        return v;
    }

    private GradientDrawable round(int color, int radiusDp) {
        GradientDrawable d = new GradientDrawable();
        d.setColor(color);
        d.setCornerRadius(dp(radiusDp));
        return d;
    }

    /** Darkened version of a status color for the live panel background. */
    private static int blend(int c) {
        int r = (Color.red(c) * 45 + Color.red(CARD) * 55) / 100;
        int g = (Color.green(c) * 45 + Color.green(CARD) * 55) / 100;
        int b = (Color.blue(c) * 45 + Color.blue(CARD) * 55) / 100;
        return Color.rgb(r, g, b);
    }

    private int dp(int v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }
}
