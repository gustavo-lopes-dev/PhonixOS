package collector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type batteryProviderFunc func() (*BatteryMetrics, error)

func (f batteryProviderFunc) GetMetrics() (*BatteryMetrics, error) { return f() }

func TestBatteryProviderFallbackOrder(t *testing.T) {
	var called []string
	metrics := collectBatteryWith([]batteryProvider{
		batteryProviderFunc(func() (*BatteryMetrics, error) {
			called = append(called, "sysfs")
			return nil, errors.New("unavailable")
		}),
		batteryProviderFunc(func() (*BatteryMetrics, error) {
			called = append(called, "dumpsys")
			return nil, nil
		}),
		batteryProviderFunc(func() (*BatteryMetrics, error) {
			called = append(called, "termux")
			return &BatteryMetrics{Source: BatterySourceTermuxAPI}, nil
		}),
		batteryProviderFunc(func() (*BatteryMetrics, error) {
			called = append(called, "mock")
			return &BatteryMetrics{Source: BatterySourceMock}, nil
		}),
	})
	if metrics.Source != BatterySourceTermuxAPI || !reflect.DeepEqual(called, []string{"sysfs", "dumpsys", "termux"}) {
		t.Fatalf("fallback = %v, providers called = %v", metrics.Source, called)
	}

	mock := collectBatteryWith(nil)
	if mock.Source != BatterySourceMock || mock.LevelPercent != 0 || mock.Status != "Unknown" || mock.Health != "Unspecified" {
		t.Fatalf("unexpected terminal fallback: %+v", mock)
	}
}

func TestBatteryProvidersParse(t *testing.T) {
	dumpsys, err := (dumpsysBatteryProvider{command: func(context.Context) ([]byte, error) {
		return []byte("level: 82\nstatus: 2\nhealth: 2\ntemperature: 315\nvoltage: 4150\nusb powered: true\n"), nil
	}}).GetMetrics()
	if err != nil || dumpsys.Source != BatterySourceDumpsys || dumpsys.LevelPercent != 82 || dumpsys.TemperatureC != 31.5 || dumpsys.VoltageMV != 4150 || !dumpsys.IsPlugged {
		t.Fatalf("dumpsys: %+v, %v", dumpsys, err)
	}

	termux, err := (termuxBatteryProvider{command: func(context.Context) ([]byte, error) {
		return []byte(`{"percentage":50,"status":"DISCHARGING","health":"GOOD","plugged":"UNPLUGGED","temperature":29.5}`), nil
	}}).GetMetrics()
	if err != nil || termux.Source != BatterySourceTermuxAPI || termux.LevelPercent != 50 || termux.Status != "Discharging" || termux.TemperatureC != 29.5 || termux.IsPlugged {
		t.Fatalf("termux: %+v, %v", termux, err)
	}
	if _, err := parseTermuxBattery([]byte("not json")); err == nil {
		t.Fatal("expected error for invalid Termux payload")
	}
	if _, err := parseDumpsysBattery("not a battery report"); err == nil {
		t.Fatal("expected error for invalid dumpsys report")
	}
}

func TestIncompleteBatteryReportsFallThrough(t *testing.T) {
	for _, input := range []string{
		`{}`, `null`, `{"percentage":null,"status":"CHARGING"}`,
		`{"percentage":42}`, `{"percentage":-1,"status":"CHARGING"}`,
		`{"percentage":101,"status":"CHARGING"}`,
	} {
		t.Run(input, func(t *testing.T) {
			metrics := collectBatteryWith([]batteryProvider{
				termuxBatteryProvider{command: func(context.Context) ([]byte, error) {
					return []byte(input), nil
				}},
				mockBatteryProvider{},
			})
			if metrics.Source != BatterySourceMock {
				t.Fatalf("incomplete Termux report selected %q instead of mock", metrics.Source)
			}
		})
	}
	for _, input := range []string{"level: 50\n", "level: 105\nstatus: 2\n"} {
		if _, err := parseDumpsysBattery(input); err == nil {
			t.Fatalf("expected incomplete dumpsys report %q to fail", input)
		}
	}
	// Campos opcionais podem estar ausentes, sem perder uma leitura válida.
	metrics, err := parseTermuxBattery([]byte(`{"percentage":0,"status":"UNKNOWN"}`))
	if err != nil || metrics.LevelPercent != 0 || metrics.Status != "Unknown" || metrics.Health != "Unspecified" {
		t.Fatalf("valid zero-percent reading: %+v, %v", metrics, err)
	}
}

func TestBatteryCommandDeadline(t *testing.T) {
	start := time.Now()
	provider := termuxBatteryProvider{command: func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	if _, err := provider.GetMetrics(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected command deadline, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > batteryCommandTimeout+time.Second {
		t.Fatalf("command took too long: %s", elapsed)
	}
}

func TestSysfsBatteryProvider(t *testing.T) {
	root := filepath.Join(t.TempDir(), "power_supply")
	battery := filepath.Join(root, "BAT0")
	usb := filepath.Join(root, "USB")
	for _, dir := range []string{battery, usb} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(battery, "type"):        "Battery\n",
		filepath.Join(battery, "capacity"):    "75\n",
		filepath.Join(battery, "status"):      "Not charging\n",
		filepath.Join(battery, "health"):      "Good\n",
		filepath.Join(battery, "temp"):        "295\n",
		filepath.Join(battery, "voltage_now"): "4150000\n",
		filepath.Join(usb, "type"):            "USB\n",
		filepath.Join(usb, "online"):          "1\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	metrics, err := (sysfsBatteryProvider{root: root}).GetMetrics()
	if err != nil || metrics.Source != BatterySourceSysFS || metrics.LevelPercent != 75 || metrics.Status != "Not charging" || metrics.TemperatureC != 29.5 || metrics.VoltageMV != 4150 || !metrics.IsPlugged {
		t.Fatalf("sysfs: %+v, %v", metrics, err)
	}
	if err := os.Remove(filepath.Join(battery, "status")); err != nil {
		t.Fatal(err)
	}
	if _, err := (sysfsBatteryProvider{root: root}).GetMetrics(); err == nil {
		t.Fatal("missing sysfs status must trigger the next battery provider")
	}
}
