package profile

import (
	"errors"
	"strings"
	"testing"
)

func props(values map[string]string) func() (map[string]string, error) {
	return func() (map[string]string, error) { return values, nil }
}

func cpuinfo(t *testing.T, text string) func() (*cpuInfo, error) {
	t.Helper()
	info, err := parseCPUInfo(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return func() (*cpuInfo, error) { return info, nil }
}

func TestParseProps(t *testing.T) {
	got := parseProps("[ro.soc.manufacturer]: [Samsung]\n[ro.soc.model]: [Exynos 2100]\ngarbage\n[empty]: []\n")
	if got["ro.soc.model"] != "Exynos 2100" || got["ro.soc.manufacturer"] != "Samsung" || len(got) != 3 {
		t.Fatalf("unexpected props %v", got)
	}
}

func TestSoCFromProps(t *testing.T) {
	cases := []struct {
		manufacturer, model, want string
	}{
		{"Samsung", "Exynos 2100", "Exynos 2100"}, // S21 (validado no aparelho)
		{"QTI", "SM8550", "Snapdragon 8 Gen 2"},   // código conhecido
		{"QTI", "SM7435", "Qualcomm SM7435"},      // código desconhecido: fabricante como prefixo
		{"Mediatek", "MT6983", "Dimensity 9000"},  // MediaTek conhecido
		{"Google", "Tensor G3", "Tensor G3"},      // nome já comercial
		{"Spreadtrum", "T610", "T610"},            // não é código conhecido: mantido
	}
	for _, tc := range cases {
		got, err := socFromProps(props(map[string]string{"ro.soc.manufacturer": tc.manufacturer, "ro.soc.model": tc.model}))
		if err != nil || got != tc.want {
			t.Errorf("%s/%s: got %q, %v; want %q", tc.manufacturer, tc.model, got, err, tc.want)
		}
	}
	if _, err := socFromProps(props(map[string]string{})); err == nil {
		t.Fatal("expected error without ro.soc.model")
	}
}

func TestPlatformFromProps(t *testing.T) {
	got, err := platformFromProps(props(map[string]string{"ro.board.platform": "kalama", "ro.hardware": "qcom"}))
	if err != nil || got != "Snapdragon 8 Gen 2" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := platformFromProps(props(map[string]string{"ro.hardware": "qcom"})); err == nil {
		t.Fatal("expected error for unknown codename")
	}
}

func TestCPUInfoSources(t *testing.T) {
	oldAndroid := cpuinfo(t, "processor\t: 0\nHardware\t: Qualcomm Technologies, Inc SM8150\n")
	if got, err := cpuinfoHardware(oldAndroid); err != nil || got != "Snapdragon 855" {
		t.Fatalf("hardware: %q, %v", got, err)
	}
	x86 := cpuinfo(t, "processor\t: 0\nmodel name\t: Intel(R) Core(TM) i7-8550U CPU @ 1.80GHz\nprocessor\t: 1\nmodel name\t: Intel(R) Core(TM) i7-8550U CPU @ 1.80GHz\n")
	if got, err := cpuinfoModelName(x86); err != nil || got != "Intel Core i7-8550U" {
		t.Fatalf("model name: %q, %v", got, err)
	}
	if _, err := cpuinfoModelName(cpuinfo(t, "model name\t: ARMv8 Processor rev 0 (v8l)\n")); err == nil {
		t.Fatal("bare ARMv8 model name must fall through")
	}
	// /proc/cpuinfo do S21 (Exynos 2100): 4× A55, 3× A78, 1× X1.
	var s21 strings.Builder
	for _, part := range []string{"0xd05", "0xd05", "0xd05", "0xd05", "0xd41", "0xd41", "0xd41", "0xd44"} {
		s21.WriteString("CPU implementer\t: 0x41\nCPU part\t: " + part + "\n\n")
	}
	if got, err := cpuinfoCores(cpuinfo(t, s21.String())); err != nil || got != "1× Cortex-X1 + 3× Cortex-A78 + 4× Cortex-A55" {
		t.Fatalf("cores: %q, %v", got, err)
	}
	if _, err := cpuinfoCores(cpuinfo(t, "CPU implementer\t: 0x51\nCPU part\t: 0x804\n")); err == nil {
		t.Fatal("unknown core must fall through")
	}
}

func TestDetectCPUModelFallsBackInOrder(t *testing.T) {
	var calls []string
	source := func(name, model string, err error) cpuModelSource {
		return cpuModelSource{name: name, detect: func() (string, error) { calls = append(calls, name); return model, err }}
	}
	got := detectCPUModelWith([]cpuModelSource{
		source("android_soc", "", errors.New("not Android")),
		source("android_platform", "", nil),
		source("cpuinfo_hardware", "Snapdragon 855", nil),
		source("unused", "x", nil),
	})
	if got == nil || *got != "Snapdragon 855" || strings.Join(calls, ",") != "android_soc,android_platform,cpuinfo_hardware" {
		t.Fatalf("got %v, calls %v", got, calls)
	}
	if detectCPUModelWith([]cpuModelSource{source("a", "", errors.New("x"))}) != nil {
		t.Fatal("expected nil when every source fails")
	}
}

func TestDetectCPUModelOnHost(t *testing.T) {
	model := DetectCPUModel()
	if model != nil {
		t.Logf("cpu model on this host: %s", *model)
	}
}
