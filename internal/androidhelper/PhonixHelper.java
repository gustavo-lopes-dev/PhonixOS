import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.PrintStream;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/**
 * PhonixOS — helper Android para contadores de tráfego.
 *
 * Executado pelo servidor Go via /system/bin/app_process (sem root) quando /proc/net/dev,
 * /sys/class/net e netlink são negados ao app (Android 10+). Percorre estratégias em ordem,
 * cada uma resolvida por reflexão para funcionar entre versões do Android:
 *
 *   por interface: netstats_iface        INetworkStatsService.getIfaceStats(String) (13+)
 *                  netstats_iface_legacy INetworkStatsService.getIfaceStats(String, int) (10–12)
 *                  trafficstats_iface    TrafficStats.getRxBytes/getTxBytes(String) (7–12)
 *   total:         netstats_total        INetworkStatsService.getTotalStats() (13+)
 *                  netstats_total_legacy INetworkStatsService.getTotalStats(int) (10–12)
 *                  trafficstats_total    TrafficStats.getTotalRxBytes/getTotalTxBytes()
 *
 * Protocolo (stdin/stdout, uma linha por mensagem):
 *   saída inicial: "ready <estratégias resolvidas>" ou "error <motivo>" (status 1);
 *   entrada: nomes de interface separados por espaço;
 *   saída: {"ifaces":{"wlan0":[rx,tx]},"iface_source":"...","total":[rx,tx],"total_source":"..."}
 *          — interfaces sem contador são omitidas; total e fontes ausentes quando indisponíveis.
 * Encerra quando stdin fecha. Argumento opcional --disable=a,b desativa estratégias
 * (validação de cada nível no aparelho).
 *
 * Build no host: scripts/build-android-helper.sh (javac + d8 do Android SDK, dex para API 24).
 */
public final class PhonixHelper {
    private static final int TYPE_RX_BYTES = 0;
    private static final int TYPE_TX_BYTES = 2;

    /** Uma forma de obter o par [rx, tx]; devolve null quando não há valor válido. */
    private abstract static class Strategy {
        final String name;

        Strategy(String name) {
            this.name = name;
        }

        abstract long[] read(String iface) throws Exception;
    }

    private PhonixHelper() {}

    public static void main(String[] args) {
        PrintStream out = System.out;
        Set<String> disabled = new HashSet<>();
        for (String arg : args) {
            if (arg.startsWith("--disable=")) {
                disabled.addAll(Arrays.asList(arg.substring("--disable=".length()).split(",")));
            }
        }

        List<Strategy> perIface = new ArrayList<>();
        List<Strategy> total = new ArrayList<>();
        Object stats = netstats();
        if (stats != null) {
            addNetstats(stats, perIface, total);
        }
        addTrafficStats(perIface, total);
        perIface.removeIf(s -> disabled.contains(s.name));
        total.removeIf(s -> disabled.contains(s.name));

        if (perIface.isEmpty() && total.isEmpty()) {
            out.println("error no traffic counter strategy available on this Android version");
            out.flush();
            System.exit(1);
            return;
        }
        StringBuilder ready = new StringBuilder("ready");
        for (Strategy s : perIface) ready.append(' ').append(s.name);
        for (Strategy s : total) ready.append(' ').append(s.name);
        out.println(ready);
        out.flush();

        try {
            BufferedReader in = new BufferedReader(new InputStreamReader(System.in));
            String line;
            while ((line = in.readLine()) != null) {
                out.println(answer(line, perIface, total));
                out.flush();
            }
        } catch (Throwable t) {
            out.println("error " + rootCause(t));
            out.flush();
            System.exit(1);
        }
    }

    private static String answer(String line, List<Strategy> perIface, List<Strategy> total) {
        StringBuilder ifaces = new StringBuilder();
        String ifaceSource = null;
        for (String name : line.trim().split("\\s+")) {
            if (name.isEmpty()) {
                continue;
            }
            for (Strategy s : perIface) {
                long[] value = attempt(s, name);
                if (value == null) {
                    continue;
                }
                if (ifaces.length() > 0) ifaces.append(',');
                ifaces.append('"').append(name).append("\":[").append(value[0]).append(',').append(value[1]).append(']');
                if (ifaceSource == null) ifaceSource = s.name;
                break;
            }
        }
        StringBuilder json = new StringBuilder("{\"ifaces\":{").append(ifaces).append('}');
        if (ifaceSource != null) {
            json.append(",\"iface_source\":\"").append(ifaceSource).append('"');
        }
        for (Strategy s : total) {
            long[] value = attempt(s, null);
            if (value != null) {
                json.append(",\"total\":[").append(value[0]).append(',').append(value[1]).append(']');
                json.append(",\"total_source\":\"").append(s.name).append('"');
                break;
            }
        }
        return json.append('}').toString();
    }

    private static long[] attempt(Strategy s, String iface) {
        try {
            long[] value = s.read(iface);
            return value != null && value[0] >= 0 && value[1] >= 0 ? value : null;
        } catch (Throwable t) {
            return null;
        }
    }

    /** INetworkStatsService pelo ServiceManager (API oculta); null se indisponível. */
    private static Object netstats() {
        try {
            Object binder = Class.forName("android.os.ServiceManager")
                    .getMethod("getService", String.class).invoke(null, "netstats");
            if (binder == null) {
                return null;
            }
            return Class.forName("android.net.INetworkStatsService$Stub")
                    .getMethod("asInterface", android.os.IBinder.class).invoke(null, binder);
        } catch (Throwable t) {
            return null;
        }
    }

    private static void addNetstats(final Object stats, List<Strategy> perIface, List<Strategy> total) {
        final Method iface = method(stats.getClass(), "getIfaceStats", String.class);
        if (iface != null) {
            perIface.add(new Strategy("netstats_iface") {
                long[] read(String name) throws Exception {
                    return statsResult(iface.invoke(stats, name));
                }
            });
        }
        final Method ifaceLegacy = method(stats.getClass(), "getIfaceStats", String.class, int.class);
        if (ifaceLegacy != null) {
            perIface.add(new Strategy("netstats_iface_legacy") {
                long[] read(String name) throws Exception {
                    return new long[] {(Long) ifaceLegacy.invoke(stats, name, TYPE_RX_BYTES),
                        (Long) ifaceLegacy.invoke(stats, name, TYPE_TX_BYTES)};
                }
            });
        }
        final Method totalStats = method(stats.getClass(), "getTotalStats");
        if (totalStats != null) {
            total.add(new Strategy("netstats_total") {
                long[] read(String name) throws Exception {
                    return statsResult(totalStats.invoke(stats));
                }
            });
        }
        final Method totalLegacy = method(stats.getClass(), "getTotalStats", int.class);
        if (totalLegacy != null) {
            total.add(new Strategy("netstats_total_legacy") {
                long[] read(String name) throws Exception {
                    return new long[] {(Long) totalLegacy.invoke(stats, TYPE_RX_BYTES),
                        (Long) totalLegacy.invoke(stats, TYPE_TX_BYTES)};
                }
            });
        }
    }

    private static void addTrafficStats(List<Strategy> perIface, List<Strategy> total) {
        Class<?> traffic;
        try {
            traffic = Class.forName("android.net.TrafficStats");
        } catch (Throwable t) {
            return;
        }
        final Method rx = method(traffic, "getRxBytes", String.class);
        final Method tx = method(traffic, "getTxBytes", String.class);
        if (rx != null && tx != null) {
            perIface.add(new Strategy("trafficstats_iface") {
                long[] read(String name) throws Exception {
                    return new long[] {(Long) rx.invoke(null, name), (Long) tx.invoke(null, name)};
                }
            });
        }
        final Method totalRx = method(traffic, "getTotalRxBytes");
        final Method totalTx = method(traffic, "getTotalTxBytes");
        if (totalRx != null && totalTx != null) {
            total.add(new Strategy("trafficstats_total") {
                long[] read(String name) throws Exception {
                    return new long[] {(Long) totalRx.invoke(null), (Long) totalTx.invoke(null)};
                }
            });
        }
    }

    /** android.net.netstats.StatsResult (campos públicos rxBytes/txBytes). */
    private static long[] statsResult(Object result) throws Exception {
        if (result == null) {
            return null;
        }
        return new long[] {result.getClass().getField("rxBytes").getLong(result),
            result.getClass().getField("txBytes").getLong(result)};
    }

    private static Method method(Class<?> type, String name, Class<?>... params) {
        try {
            return type.getMethod(name, params);
        } catch (Throwable t) {
            return null;
        }
    }

    private static String rootCause(Throwable t) {
        while (t.getCause() != null) {
            t = t.getCause();
        }
        return String.valueOf(t).replace('\n', ' ');
    }
}
