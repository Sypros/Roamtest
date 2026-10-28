package be.roamtest;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.net.wifi.WifiInfo;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.os.IBinder;
import android.os.PowerManager;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.File;
import java.io.FileOutputStream;
import java.io.OutputStream;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.HttpURLConnection;
import java.net.InetAddress;
import java.net.SocketTimeoutException;
import java.net.URL;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.text.SimpleDateFormat;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Date;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Set;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;

/**
 * Simulates a VoIP call: one 172-byte UDP packet every 20 ms to the laptop,
 * which echoes it back. Every second it records loss, latency, jitter, the
 * longest audio gap and the access point, then uploads the run when stopped.
 */
public class TestService extends Service {

    static final String ACTION_START = "be.roamtest.START";
    static final String ACTION_STOP = "be.roamtest.STOP";
    static final String ACTION_MARK = "be.roamtest.MARK";
    static final String EXTRA_LABEL = "label";
    static final String EXTRA_SERVER = "server";

    static final int UDP_PORT = 5005;
    static final int HTTP_PORT = 8080;
    private static final String CHANNEL = "roamtest";
    private static final int NOTIF_ID = 1;

    private ScheduledExecutorService exec;
    private Thread receiver;
    private DatagramSocket socket;
    private PowerManager.WakeLock wakeLock;
    private WifiManager.WifiLock wifiLock;
    private WifiManager wifi;

    private final Object lock = new Object();
    private int winTx, winRx, totTx, totRx;
    private final List<Double> winRtt = new ArrayList<>();
    private long winGapNs;
    private long lastRxNs;
    private int seq;

    private final List<JSONObject> rows = new ArrayList<>();
    private String prevBssid;
    private volatile String pendingMark;
    private long startMs;

    @Override
    public IBinder onBind(Intent intent) {
        return null;
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent != null ? intent.getAction() : null;
        if (ACTION_START.equals(action) && !TestState.running) {
            start(intent.getStringExtra(EXTRA_LABEL), intent.getStringExtra(EXTRA_SERVER));
        } else if (ACTION_STOP.equals(action)) {
            stop();
        } else if (ACTION_MARK.equals(action) && TestState.running) {
            TestState.marks++;
            pendingMark = "Spot " + TestState.marks;
        } else if (!TestState.running) {
            stopSelf();
        }
        return START_NOT_STICKY;
    }

    // ---------------------------------------------------------------- start

    private void start(String label, String server) {
        startInForeground();
        TestState.reset(label, server);
        TestState.running = true;
        startMs = System.currentTimeMillis();
        rows.clear();
        prevBssid = null;
        pendingMark = null;
        winTx = winRx = totTx = totRx = 0;
        winRtt.clear();
        winGapNs = 0;
        lastRxNs = System.nanoTime();
        seq = 0;

        PowerManager pm = (PowerManager) getSystemService(Context.POWER_SERVICE);
        wakeLock = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "roamtest:test");
        wakeLock.acquire(4 * 60 * 60 * 1000L);
        wifi = (WifiManager) getApplicationContext().getSystemService(Context.WIFI_SERVICE);
        int mode = Build.VERSION.SDK_INT >= 29
                ? WifiManager.WIFI_MODE_FULL_LOW_LATENCY
                : WifiManager.WIFI_MODE_FULL_HIGH_PERF;
        wifiLock = wifi.createWifiLock(mode, "roamtest:wifi");
        wifiLock.acquire();

        exec = Executors.newScheduledThreadPool(2);
        exec.execute(() -> {
            try {
                InetAddress addr = InetAddress.getByName(server);
                socket = new DatagramSocket();
                socket.setSoTimeout(200);
                socket.connect(addr, UDP_PORT);
            } catch (Exception e) {
                TestState.status = "Cannot reach " + server + ": " + e.getMessage();
                return;
            }
            exec.scheduleAtFixedRate(this::sendOne, 0, 20, TimeUnit.MILLISECONDS);
            exec.scheduleAtFixedRate(this::tick, 1, 1, TimeUnit.SECONDS);
            receiver = new Thread(this::receiveLoop, "rx");
            receiver.start();
        });
    }

    private void startInForeground() {
        NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
        nm.createNotificationChannel(new NotificationChannel(CHANNEL, "Walk test",
                NotificationManager.IMPORTANCE_LOW));
        Intent open = new Intent(this, MainActivity.class);
        PendingIntent pi = PendingIntent.getActivity(this, 0, open, PendingIntent.FLAG_IMMUTABLE);
        Intent mark = new Intent(this, TestService.class).setAction(ACTION_MARK);
        PendingIntent mi = PendingIntent.getService(this, 1, mark, PendingIntent.FLAG_IMMUTABLE);
        Intent stop = new Intent(this, TestService.class).setAction(ACTION_STOP);
        PendingIntent si = PendingIntent.getService(this, 2, stop, PendingIntent.FLAG_IMMUTABLE);
        Notification n = new Notification.Builder(this, CHANNEL)
                .setSmallIcon(R.drawable.ic_launcher)
                .setContentTitle("RoamTest running")
                .setContentText("Walk your route. Tap Mark at each spot.")
                .setContentIntent(pi)
                .setOngoing(true)
                .addAction(new Notification.Action.Builder(null, "Mark spot", mi).build())
                .addAction(new Notification.Action.Builder(null, "Stop", si).build())
                .build();
        if (Build.VERSION.SDK_INT >= 29) {
            startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION);
        } else {
            startForeground(NOTIF_ID, n);
        }
    }

    // -------------------------------------------------------------- traffic

    private void sendOne() {
        DatagramSocket s = socket;
        if (s == null) return;
        ByteBuffer b = ByteBuffer.allocate(172);
        b.putInt(seq++);
        b.putLong(System.nanoTime());
        try {
            s.send(new DatagramPacket(b.array(), b.capacity()));
        } catch (Exception ignored) {
            // network briefly gone during a roam
        }
        synchronized (lock) {
            winTx++;
            totTx++;
        }
    }

    private void receiveLoop() {
        byte[] buf = new byte[2048];
        DatagramPacket p = new DatagramPacket(buf, buf.length);
        while (TestState.running) {
            try {
                socket.receive(p);
            } catch (SocketTimeoutException e) {
                continue;
            } catch (Exception e) {
                if (!TestState.running) return;
                continue;
            }
            if (p.getLength() < 12) continue;
            long now = System.nanoTime();
            long sent = ByteBuffer.wrap(buf, 4, 8).getLong();
            double rttMs = (now - sent) / 1e6;
            synchronized (lock) {
                winRx++;
                totRx++;
                winRtt.add(rttMs);
                long g = now - lastRxNs;
                if (g > winGapNs) winGapNs = g;
                lastRxNs = now;
            }
        }
    }

    // ------------------------------------------------------- once a second

    private void tick() {
        try {
            long now = System.nanoTime();
            int tx, rx;
            List<Double> rtts;
            long gapNs;
            synchronized (lock) {
                tx = winTx;
                rx = winRx;
                rtts = new ArrayList<>(winRtt);
                gapNs = Math.max(winGapNs, now - lastRxNs);
                winTx = 0;
                winRx = 0;
                winRtt.clear();
                winGapNs = 0;
            }
            double loss = tx > 0 ? Math.max(0, 100.0 * (1 - (double) rx / tx)) : 0;
            double avg = 0, jit = 0;
            if (!rtts.isEmpty()) {
                for (double r : rtts) avg += r;
                avg /= rtts.size();
                for (int i = 1; i < rtts.size(); i++) jit += Math.abs(rtts.get(i) - rtts.get(i - 1));
                if (rtts.size() > 1) jit /= (rtts.size() - 1);
            }
            double gapMs = Math.round(gapNs / 1e6);

            String b = "?";
            int rssi = 0, freq = 0;
            WifiInfo wi = wifi.getConnectionInfo();
            if (wi != null && wi.getBSSID() != null && !"02:00:00:00:00:00".equals(wi.getBSSID())) {
                b = wi.getBSSID();
                rssi = wi.getRssi();
                freq = wi.getFrequency();
            }
            String event = "";
            if (prevBssid != null && !b.equals(prevBssid) && !"?".equals(b)) {
                event = "ROAM " + prevBssid + " -> " + b;
                TestState.roams++;
            }
            if (!"?".equals(b)) prevBssid = b;

            String mark = pendingMark;
            pendingMark = null;

            int t = rows.size() + 1;
            JSONObject row = new JSONObject();
            row.put("t", t);
            row.put("time", new SimpleDateFormat("HH:mm:ss", Locale.US).format(new Date()));
            row.put("bssid", b);
            row.put("rssi", rssi);
            row.put("freq", freq);
            row.put("tx", tx);
            row.put("rx", rx);
            row.put("loss", round1(loss));
            row.put("rtt", round1(avg));
            row.put("jitter", round1(jit));
            row.put("gap", gapMs);
            row.put("event", event);
            row.put("mark", mark == null ? "" : mark);
            rows.add(row);

            if (loss > 5 || gapMs > 150) TestState.badSeconds++;
            TestState.seconds = t;
            TestState.bssid = b;
            TestState.rssi = rssi;
            TestState.freq = freq;
            TestState.loss = loss;
            TestState.rtt = avg;
            TestState.gap = gapMs;
            TestState.noReplies = totTx > 150 && totRx == 0;
        } catch (Exception e) {
            TestState.status = "Error: " + e.getMessage();
        }
    }

    // ----------------------------------------------------------------- stop

    private void stop() {
        if (!TestState.running) {
            stopSelf();
            return;
        }
        TestState.running = false;
        if (exec != null) exec.shutdownNow();
        if (socket != null) socket.close();
        if (wifiLock != null && wifiLock.isHeld()) wifiLock.release();
        if (wakeLock != null && wakeLock.isHeld()) wakeLock.release();

        final String server = TestState.server;
        new Thread(() -> {
            try {
                // wait for the tick thread to finish writing
                if (exec != null) exec.awaitTermination(2, TimeUnit.SECONDS);
                JSONObject run = new JSONObject();
                JSONObject s = summarize();
                run.put("summary", s);
                JSONArray arr = new JSONArray();
                for (JSONObject r : rows) arr.put(r);
                run.put("rows", arr);

                File dir = pendingDir(this);
                File f = new File(dir, s.getString("id") + ".json");
                try (OutputStream os = new FileOutputStream(f)) {
                    os.write(run.toString().getBytes(StandardCharsets.UTF_8));
                }
                String text = String.format(Locale.US,
                        "%s: %d roams, avg roam gap %.0f ms, worst gap %.0f ms, loss %.1f%%",
                        s.getString("label"), s.getInt("roams"), s.getDouble("avg_roam_gap_ms"),
                        s.getDouble("worst_gap_ms"), s.getDouble("loss_pct"));
                if (upload(server, f)) {
                    TestState.status = text + "\nUploaded to laptop.";
                } else {
                    TestState.status = text + "\nUpload failed. Saved on phone; tap 'Upload saved runs' later.";
                }
            } catch (Exception e) {
                TestState.status = "Could not save run: " + e.getMessage();
            }
            stopForeground(STOP_FOREGROUND_REMOVE);
            stopSelf();
        }).start();
    }

    private JSONObject summarize() throws Exception {
        Date start = new Date(startMs);
        String label = TestState.label;
        JSONObject s = new JSONObject();
        s.put("id", new SimpleDateFormat("yyyyMMdd_HHmmss", Locale.US).format(start) + "_" + safe(label));
        s.put("label", label);
        s.put("date", new SimpleDateFormat("yyyy-MM-dd HH:mm", Locale.US).format(start));
        s.put("device", Build.MANUFACTURER + " " + Build.MODEL);
        s.put("duration_s", rows.size());

        List<Double> roamGaps = new ArrayList<>();
        List<Integer> rssis = new ArrayList<>();
        Set<String> aps = new HashSet<>();
        double worst = 0;
        int bad = 0;
        for (int i = 0; i < rows.size(); i++) {
            JSONObject r = rows.get(i);
            double g = r.getDouble("gap");
            worst = Math.max(worst, g);
            if (r.getDouble("loss") > 5 || g > 150) bad++;
            if (r.getInt("rssi") != 0) rssis.add(r.getInt("rssi"));
            String b = r.getString("bssid");
            if (!"?".equals(b)) aps.add(b);
            if (!r.getString("event").isEmpty()) {
                // a roam's dropout can land in the second before or after the switch
                double rg = g;
                if (i > 0) rg = Math.max(rg, rows.get(i - 1).getDouble("gap"));
                if (i + 1 < rows.size()) rg = Math.max(rg, rows.get(i + 1).getDouble("gap"));
                roamGaps.add(rg);
            }
        }
        double lossPct = totTx > 0 ? Math.max(0, 100.0 * (1 - (double) totRx / totTx)) : 0;
        double avgRoam = 0, worstRoam = 0;
        for (double g : roamGaps) {
            avgRoam += g;
            worstRoam = Math.max(worstRoam, g);
        }
        if (!roamGaps.isEmpty()) avgRoam = Math.round(avgRoam / roamGaps.size());
        double median = 0;
        if (!rssis.isEmpty()) {
            Collections.sort(rssis);
            median = rssis.get(rssis.size() / 2);
        }
        s.put("loss_pct", Math.round(lossPct * 100) / 100.0);
        s.put("roams", roamGaps.size());
        s.put("avg_roam_gap_ms", avgRoam);
        s.put("worst_roam_gap_ms", worstRoam);
        s.put("worst_gap_ms", worst);
        s.put("bad_seconds", bad);
        s.put("median_rssi", median);
        s.put("aps", aps.size());
        s.put("marks", TestState.marks);
        return s;
    }

    // -------------------------------------------------------------- helpers

    static File pendingDir(Context c) {
        File d = new File(c.getFilesDir(), "pending");
        d.mkdirs();
        return d;
    }

    /** Uploads one saved run; on success moves it out of the pending folder. */
    static boolean upload(String server, File f) {
        HttpURLConnection con = null;
        try {
            byte[] data = Files.readAllBytes(f.toPath());
            con = (HttpURLConnection) new URL("http://" + server + ":" + HTTP_PORT + "/upload").openConnection();
            con.setConnectTimeout(5000);
            con.setReadTimeout(10000);
            con.setDoOutput(true);
            con.setRequestMethod("POST");
            con.setRequestProperty("Content-Type", "application/json");
            try (OutputStream os = con.getOutputStream()) {
                os.write(data);
            }
            if (con.getResponseCode() != 200) return false;
            File done = new File(f.getParentFile().getParentFile(), "uploaded");
            done.mkdirs();
            //noinspection ResultOfMethodCallIgnored
            f.renameTo(new File(done, f.getName()));
            return true;
        } catch (Exception e) {
            return false;
        } finally {
            if (con != null) con.disconnect();
        }
    }

    private static double round1(double v) {
        return Math.round(v * 10) / 10.0;
    }

    private static String safe(String s) {
        StringBuilder b = new StringBuilder();
        for (char c : s.toCharArray()) {
            b.append(Character.isLetterOrDigit(c) || c == '-' || c == '_' ? c : '-');
        }
        return b.length() == 0 ? "run" : b.toString();
    }
}
