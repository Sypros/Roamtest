package be.roamtest;

/** Live values shared between the measuring service and the screen. */
final class TestState {
    private TestState() {}

    static volatile boolean running;
    static volatile String label = "";
    static volatile String server = "";

    static volatile int seconds;
    static volatile String bssid = "?";
    static volatile int rssi;
    static volatile int freq;
    static volatile double loss;
    static volatile double rtt;
    static volatile double gap;
    static volatile int roams;
    static volatile int marks;
    static volatile int badSeconds;
    static volatile boolean noReplies;

    /** Message shown under the buttons: summary, upload result, errors. */
    static volatile String status = "";

    static void reset(String lbl, String srv) {
        label = lbl;
        server = srv;
        seconds = 0;
        bssid = "?";
        rssi = 0;
        freq = 0;
        loss = 0;
        rtt = 0;
        gap = 0;
        roams = 0;
        marks = 0;
        badSeconds = 0;
        noReplies = false;
        status = "";
    }

    static String band(int f) {
        if (f >= 5925) return "6 GHz";
        if (f >= 5000) return "5 GHz";
        if (f > 0) return "2.4 GHz";
        return "-";
    }
}
