package profile

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	cpuInfoPath    = "/proc/cpuinfo"
	getpropTimeout = 2 * time.Second
)

// getpropPaths: caminho absoluto evita exec.LookPath (faccessat2 → SIGSYS sob o
// seccomp do Android em binários GOOS=linux) e não depende de /system/bin no PATH.
var getpropPaths = []string{"/system/bin/getprop"}

// cpuModelSource é um nível da cadeia de identificação do processador.
type cpuModelSource struct {
	name   string
	detect func() (string, error)
}

// DetectCPUModel identifica o nome do processador percorrendo, em ordem, as
// propriedades do Android (SoC e codinome da plataforma) e o /proc/cpuinfo
// (Hardware, model name e composição dos núcleos ARM). Devolve nil quando
// nenhuma fonte responde; a falha nunca impede o boot.
func DetectCPUModel() *string {
	props := lazyProps(readAndroidProps)
	cpuinfo := lazyCPUInfo(cpuInfoPath)
	return detectCPUModelWith([]cpuModelSource{
		{name: "android_soc", detect: func() (string, error) { return socFromProps(props) }},
		{name: "android_platform", detect: func() (string, error) { return platformFromProps(props) }},
		{name: "cpuinfo_hardware", detect: func() (string, error) { return cpuinfoHardware(cpuinfo) }},
		{name: "cpuinfo_model_name", detect: func() (string, error) { return cpuinfoModelName(cpuinfo) }},
		{name: "cpuinfo_cores", detect: func() (string, error) { return cpuinfoCores(cpuinfo) }},
	})
}

func detectCPUModelWith(sources []cpuModelSource) *string {
	for _, source := range sources {
		model, err := source.detect()
		if err == nil && model != "" {
			slog.Info("profile: cpu model detected", "source", source.name, "model", model)
			return &model
		}
		slog.Debug("profile: cpu model source failed, falling back", "source", source.name, "error", err)
	}
	slog.Info("profile: cpu model unavailable")
	return nil
}

// --- Propriedades do Android -------------------------------------------------

var propLine = regexp.MustCompile(`^\[([^\]]+)\]: \[(.*)\]$`)

func lazyProps(read func() (map[string]string, error)) func() (map[string]string, error) {
	var props map[string]string
	var err error
	done := false
	return func() (map[string]string, error) {
		if !done {
			props, err = read()
			done = true
		}
		return props, err
	}
}

// readAndroidProps executa `getprop` uma única vez e interpreta "[chave]: [valor]".
func readAndroidProps() (map[string]string, error) {
	path := ""
	for _, candidate := range getpropPaths {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			path = candidate
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("getprop not found (not Android)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), getpropTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.WaitDelay = getpropTimeout
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("getprop: %w", err)
	}
	return parseProps(string(output)), nil
}

func parseProps(output string) map[string]string {
	props := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if match := propLine.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			props[match[1]] = strings.TrimSpace(match[2])
		}
	}
	return props
}

// socFromProps usa ro.soc.manufacturer/ro.soc.model (Android 12+).
func socFromProps(props func() (map[string]string, error)) (string, error) {
	values, err := props()
	if err != nil {
		return "", err
	}
	model := values["ro.soc.model"]
	if model == "" {
		return "", fmt.Errorf("ro.soc.model not set")
	}
	return normalizeSoC(values["ro.soc.manufacturer"], model), nil
}

// platformFromProps traduz o codinome da plataforma (Android anterior ao 12).
func platformFromProps(props func() (map[string]string, error)) (string, error) {
	values, err := props()
	if err != nil {
		return "", err
	}
	for _, key := range []string{"ro.hardware.chipname", "ro.board.platform", "ro.hardware", "ro.product.board"} {
		if name, ok := lookupSoC(values[key]); ok {
			return name, nil
		}
	}
	return "", fmt.Errorf("no known platform codename")
}

// --- /proc/cpuinfo -------------------------------------------------------------

type cpuInfo struct {
	fields map[string][]string
}

func lazyCPUInfo(path string) func() (*cpuInfo, error) {
	var info *cpuInfo
	var err error
	done := false
	return func() (*cpuInfo, error) {
		if !done {
			info, err = readCPUInfo(path)
			done = true
		}
		return info, err
	}
}

func readCPUInfo(path string) (*cpuInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			slog.Debug("profile: close cpuinfo", "error", closeErr)
		}
	}()
	return parseCPUInfo(file)
}

func parseCPUInfo(file io.Reader) (*cpuInfo, error) {
	info := &cpuInfo{fields: make(map[string][]string)}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		info.fields[key] = append(info.fields[key], strings.TrimSpace(value))
	}
	return info, scanner.Err()
}

func (c *cpuInfo) first(key string) string {
	if values := c.fields[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}

var socCodeToken = regexp.MustCompile(`(?i)\b(SM\d{4}|SDM\d{3}|MSM\d{4}|MT\d{4})\b`)

// cpuinfoHardware lê a linha "Hardware" (kernels Android antigos), ex.:
// "Qualcomm Technologies, Inc SM8150" → "Snapdragon 855".
func cpuinfoHardware(load func() (*cpuInfo, error)) (string, error) {
	info, err := load()
	if err != nil {
		return "", err
	}
	hardware := info.first("hardware")
	if hardware == "" {
		return "", fmt.Errorf("no Hardware line")
	}
	if code := socCodeToken.FindString(hardware); code != "" {
		if name, ok := lookupSoC(code); ok {
			return name, nil
		}
	}
	if name, ok := lookupSoC(hardware); ok {
		return name, nil
	}
	return cleanModelName(hardware), nil
}

// cpuinfoModelName lê "model name" (Linux x86 e ARM32).
func cpuinfoModelName(load func() (*cpuInfo, error)) (string, error) {
	info, err := load()
	if err != nil {
		return "", err
	}
	name := cleanModelName(info.first("model name"))
	// ARM64 às vezes expõe só a arquitetura ("ARMv8 Processor rev 0 (v8l)"): pouco
	// informativo, a composição dos núcleos é preferível.
	if name == "" || strings.HasPrefix(strings.ToLower(name), "armv") {
		return "", fmt.Errorf("no descriptive model name")
	}
	return name, nil
}

// cpuinfoCores descreve os núcleos ARM pelo "CPU part" (ex.: "1× Cortex-X1 +
// 3× Cortex-A78 + 4× Cortex-A55"), do mais rápido para o mais econômico.
func cpuinfoCores(load func() (*cpuInfo, error)) (string, error) {
	info, err := load()
	if err != nil {
		return "", err
	}
	implementers := info.fields["cpu implementer"]
	parts := info.fields["cpu part"]
	if len(parts) == 0 || len(implementers) != len(parts) {
		return "", fmt.Errorf("no CPU part lines")
	}
	counts := make(map[string]int)
	var order []string
	for i, part := range parts {
		name, ok := armCores[strings.ToLower(implementers[i])+":"+strings.ToLower(part)]
		if !ok {
			return "", fmt.Errorf("unknown core %s:%s", implementers[i], part)
		}
		if counts[name] == 0 {
			order = append(order, name)
		}
		counts[name]++
	}
	sort.SliceStable(order, func(i, j int) bool { return armCoreRank[order[i]] > armCoreRank[order[j]] })
	groups := make([]string, 0, len(order))
	for _, name := range order {
		groups = append(groups, fmt.Sprintf("%d× %s", counts[name], name))
	}
	return strings.Join(groups, " + "), nil
}

var spaces = regexp.MustCompile(`\s+`)

// cleanModelName remove marcas registradas e sufixos de frequência do nome.
func cleanModelName(name string) string {
	name = strings.NewReplacer("(R)", "", "(r)", "", "(TM)", "", "(tm)", "", " CPU ", " ").Replace(name)
	if at := strings.Index(name, " @ "); at > 0 {
		name = name[:at]
	}
	return strings.TrimSpace(spaces.ReplaceAllString(name, " "))
}

// --- Nomes comerciais ----------------------------------------------------------

// normalizeSoC transforma fabricante + modelo do SoC em nome comercial. Modelos
// já comerciais (ex.: Samsung "Exynos 2100", Google "Tensor G3") são mantidos;
// códigos desconhecidos recebem o fabricante como prefixo ("Qualcomm SM7435").
func normalizeSoC(manufacturer, model string) string {
	if name, ok := lookupSoC(model); ok {
		return name
	}
	brand := map[string]string{"qti": "Qualcomm", "qualcomm": "Qualcomm", "mediatek": "MediaTek", "samsung": "Samsung", "google": "Google"}[strings.ToLower(manufacturer)]
	if brand == "" {
		brand = manufacturer
	}
	if socCodeToken.MatchString(model) && brand != "" && !strings.Contains(strings.ToLower(model), strings.ToLower(brand)) {
		return brand + " " + model
	}
	return model
}

func lookupSoC(code string) (string, bool) {
	name, ok := socNames[strings.ToLower(strings.TrimSpace(code))]
	return name, ok && code != ""
}

// socNames mapeia códigos e codinomes de plataforma sem ambiguidade para o nome
// comercial. Códigos ausentes caem no nome cru (ainda informativo).
var socNames = map[string]string{
	// Qualcomm (modelo do SoC)
	"sm8750": "Snapdragon 8 Elite", "sm8650": "Snapdragon 8 Gen 3", "sm8635": "Snapdragon 8s Gen 3",
	"sm8550": "Snapdragon 8 Gen 2", "sm8475": "Snapdragon 8+ Gen 1", "sm8450": "Snapdragon 8 Gen 1",
	"sm8350": "Snapdragon 888", "sm8250": "Snapdragon 865", "sm8150": "Snapdragon 855",
	"sdm845": "Snapdragon 845", "msm8998": "Snapdragon 835", "msm8996": "Snapdragon 820",
	"sm7675": "Snapdragon 7+ Gen 3", "sm7550": "Snapdragon 7 Gen 3", "sm7475": "Snapdragon 7+ Gen 2",
	"sm7450": "Snapdragon 7 Gen 1", "sm7325": "Snapdragon 778G", "sm7250": "Snapdragon 765G",
	"sm6450": "Snapdragon 6 Gen 1", "sm6375": "Snapdragon 695", "sm6225": "Snapdragon 680",
	"sm6115": "Snapdragon 662", "sm4350": "Snapdragon 480", "sdm660": "Snapdragon 660",
	"msm8953": "Snapdragon 625", "sdm450": "Snapdragon 450",
	// Qualcomm (codinome da plataforma)
	"sun": "Snapdragon 8 Elite", "pineapple": "Snapdragon 8 Gen 3", "kalama": "Snapdragon 8 Gen 2",
	"cape": "Snapdragon 8+ Gen 1", "taro": "Snapdragon 8 Gen 1", "lahaina": "Snapdragon 888",
	"kona": "Snapdragon 865", "msmnile": "Snapdragon 855",
	// MediaTek
	"mt6991": "Dimensity 9400", "mt6989": "Dimensity 9300", "mt6985": "Dimensity 9200",
	"mt6983": "Dimensity 9000", "mt6895": "Dimensity 8100", "mt6893": "Dimensity 1200",
	"mt6877": "Dimensity 900", "mt6833": "Dimensity 700", "mt6785": "Helio G90T",
	// Samsung Exynos (modelo e codinome)
	"exynos2400": "Exynos 2400", "s5e9945": "Exynos 2400", "exynos2200": "Exynos 2200", "s5e9925": "Exynos 2200",
	"exynos2100": "Exynos 2100", "s5e9840": "Exynos 2100", "exynos990": "Exynos 990",
	"exynos9825": "Exynos 9825", "exynos9820": "Exynos 9820", "exynos9810": "Exynos 9810",
	"exynos1380": "Exynos 1380", "s5e8835": "Exynos 1380", "exynos1280": "Exynos 1280", "s5e8825": "Exynos 1280",
	// Google Tensor
	"gs101": "Google Tensor", "gs201": "Google Tensor G2", "zuma": "Google Tensor G3", "zumapro": "Google Tensor G4",
}

// armCores: "<implementer>:<part>" de /proc/cpuinfo → núcleo ARM.
var armCores = map[string]string{
	"0x41:0xd03": "Cortex-A53", "0x41:0xd04": "Cortex-A35", "0x41:0xd05": "Cortex-A55",
	"0x41:0xd07": "Cortex-A57", "0x41:0xd08": "Cortex-A72", "0x41:0xd09": "Cortex-A73",
	"0x41:0xd0a": "Cortex-A75", "0x41:0xd0b": "Cortex-A76", "0x41:0xd0d": "Cortex-A77",
	"0x41:0xd41": "Cortex-A78", "0x41:0xd44": "Cortex-X1", "0x41:0xd46": "Cortex-A510",
	"0x41:0xd47": "Cortex-A710", "0x41:0xd48": "Cortex-X2", "0x41:0xd4d": "Cortex-A715",
	"0x41:0xd4e": "Cortex-X3", "0x41:0xd80": "Cortex-A520", "0x41:0xd81": "Cortex-A720",
	"0x41:0xd82": "Cortex-X4", "0x41:0xc07": "Cortex-A7", "0x41:0xc0f": "Cortex-A15",
}

// armCoreRank ordena os grupos do núcleo mais rápido para o mais econômico.
var armCoreRank = map[string]int{
	"Cortex-X4": 100, "Cortex-X3": 99, "Cortex-X2": 98, "Cortex-X1": 97,
	"Cortex-A720": 90, "Cortex-A715": 89, "Cortex-A710": 88, "Cortex-A78": 87, "Cortex-A77": 86,
	"Cortex-A76": 85, "Cortex-A75": 84, "Cortex-A73": 83, "Cortex-A72": 82, "Cortex-A57": 81,
	"Cortex-A15": 80, "Cortex-A520": 20, "Cortex-A510": 19, "Cortex-A55": 18, "Cortex-A53": 17,
	"Cortex-A35": 16, "Cortex-A7": 15,
}
