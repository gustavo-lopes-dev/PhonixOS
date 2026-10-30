// Package androidhelper executa o helper Java (PhonixHelper.java neste diretório, compilado para dex no
// host e incorporado ao binário) via /system/bin/app_process para ler contadores de
// tráfego do serviço "netstats" do Android, último recurso quando /proc/net/dev,
// /sys/class/net e netlink são negados ao app. O processo (~50 MB de RAM) só existe
// enquanto há consultas: é iniciado na primeira e encerrado após idleTimeout sem uso.
package androidhelper

import (
	"bufio"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	dexName        = "phonix-helper.dex"
	mainClass      = "PhonixHelper"
	startTimeout   = 15 * time.Second
	requestTimeout = 3 * time.Second
	idleTimeout    = 20 * time.Second
	failureBackoff = time.Minute
)

// appProcessPaths são os executáveis do runtime Android, por ordem de preferência.
var appProcessPaths = []string{"/system/bin/app_process64", "/system/bin/app_process"}

// dex é gerado por scripts/build-android-helper.sh antes do go build; num checkout sem
// ele, o diretório contém só .gitkeep e o helper fica indisponível.
//
//go:embed all:dex
var dex embed.FS

// validName limita os nomes enviados ao protocolo de linha (sem espaços nem aspas).
var validName = regexp.MustCompile(`^[A-Za-z0-9_.:@-]+$`)

// Counters são os bytes acumulados de uma interface (ou do aparelho, no total).
type Counters struct {
	RecvBytes uint64
	SentBytes uint64
}

// Result reúne a resposta do helper: contadores por interface e o total do aparelho,
// cada um com o nome da estratégia que respondeu (ver PhonixHelper.java).
type Result struct {
	Ifaces      map[string]Counters
	IfaceSource string
	Total       *Counters
	TotalSource string
}

// ErrUnavailable indica que o helper não pode rodar neste ambiente (não Android ou dex
// ausente no build).
var ErrUnavailable = errors.New("androidhelper: unavailable")

// Client mantém um único processo helper e serializa as consultas.
type Client struct {
	dir string

	mu          sync.Mutex
	proc        *process
	idle        *time.Timer
	failedUntil time.Time
	lastErr     string
}

type process struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	lines      chan string
	strategies string
}

// Default usa $HOME/.phonixos/android como diretório de trabalho do runtime.
var Default = &Client{dir: defaultDir()}

func defaultDir() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.TempDir()
	}
	return filepath.Join(home, ".phonixos", "android")
}

// InterfaceCounters consulta os contadores das interfaces informadas e o total do
// aparelho. Interfaces sem contador são omitidas; Total é nil quando indisponível.
func (c *Client) InterfaceCounters(names []string) (*Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Now().Before(c.failedUntil) {
		return nil, fmt.Errorf("androidhelper: backing off after failure: %s", c.lastErr)
	}
	valid := make([]string, 0, len(names))
	for _, name := range names {
		if validName.MatchString(name) {
			valid = append(valid, name)
		}
	}
	if len(valid) == 0 {
		return nil, errors.New("androidhelper: no interface names to query")
	}

	if c.proc == nil {
		proc, err := c.start()
		if err != nil {
			if !errors.Is(err, ErrUnavailable) {
				c.fail(err)
			}
			return nil, err
		}
		c.proc = proc
		slog.Info("androidhelper: started", "pid", proc.cmd.Process.Pid, "strategies", proc.strategies)
	}
	c.touch()

	counters, err := c.query(valid)
	if err != nil {
		c.fail(err)
		return nil, err
	}
	return counters, nil
}

// response espelha a linha JSON do helper.
type response struct {
	Ifaces      map[string][2]int64 `json:"ifaces"`
	IfaceSource string              `json:"iface_source"`
	Total       *[2]int64           `json:"total"`
	TotalSource string              `json:"total_source"`
}

func (c *Client) query(names []string) (*Result, error) {
	if _, err := io.WriteString(c.proc.stdin, strings.Join(names, " ")+"\n"); err != nil {
		return nil, fmt.Errorf("androidhelper: write request: %w", err)
	}
	line, err := c.proc.readLine(requestTimeout)
	if err != nil {
		return nil, err
	}
	return parseResponse(line)
}

func parseResponse(line string) (*Result, error) {
	var raw response
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("androidhelper: invalid response %q: %w", line, err)
	}
	result := &Result{Ifaces: make(map[string]Counters, len(raw.Ifaces)), IfaceSource: raw.IfaceSource}
	for name, pair := range raw.Ifaces {
		if pair[0] >= 0 && pair[1] >= 0 {
			result.Ifaces[name] = Counters{RecvBytes: uint64(pair[0]), SentBytes: uint64(pair[1])}
		}
	}
	if raw.Total != nil && raw.Total[0] >= 0 && raw.Total[1] >= 0 {
		result.Total = &Counters{RecvBytes: uint64(raw.Total[0]), SentBytes: uint64(raw.Total[1])}
		result.TotalSource = raw.TotalSource
	}
	return result, nil
}

// fail encerra o processo e suspende novas tentativas por failureBackoff.
func (c *Client) fail(err error) {
	c.stopLocked()
	c.failedUntil = time.Now().Add(failureBackoff)
	if msg := err.Error(); msg != c.lastErr {
		slog.Warn("androidhelper: failed, retrying later", "error", err, "retry_in", failureBackoff)
		c.lastErr = msg
	}
}

// touch (re)arma o encerramento por ociosidade.
func (c *Client) touch() {
	if c.idle != nil {
		c.idle.Stop()
	}
	proc := c.proc
	c.idle = time.AfterFunc(idleTimeout, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.proc == proc {
			slog.Info("androidhelper: stopped after idle period")
			c.stopLocked()
		}
	})
}

// Stop encerra o helper, se estiver em execução.
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopLocked()
}

func (c *Client) stopLocked() {
	if c.idle != nil {
		c.idle.Stop()
		c.idle = nil
	}
	if c.proc == nil {
		return
	}
	proc := c.proc
	c.proc = nil
	if err := proc.stdin.Close(); err != nil {
		slog.Debug("androidhelper: close stdin", "error", err)
	}
	if err := syscall.Kill(-proc.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		slog.Debug("androidhelper: kill", "error", err)
	}
	go func() {
		if err := proc.cmd.Wait(); err != nil {
			slog.Debug("androidhelper: exited", "error", err)
		}
	}()
}

func (c *Client) start() (*process, error) {
	runtime := ""
	for _, path := range appProcessPaths {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			runtime = path
			break
		}
	}
	if runtime == "" {
		return nil, fmt.Errorf("%w: app_process not found", ErrUnavailable)
	}
	payload, err := dex.ReadFile("dex/" + dexName)
	if err != nil {
		return nil, fmt.Errorf("%w: helper dex not embedded in this build", ErrUnavailable)
	}
	dexPath, err := c.writeDex(payload)
	if err != nil {
		return nil, err
	}
	dalvikCache := filepath.Join(c.dir, "dalvik-cache")
	if err := os.MkdirAll(dalvikCache, 0o700); err != nil {
		return nil, fmt.Errorf("androidhelper: create %q: %w", dalvikCache, err)
	}

	// Caminho absoluto: exec.LookPath causaria SIGSYS sob o seccomp do Android.
	cmd := exec.Command(runtime, "/", mainClass)
	cmd.Env = append(filteredEnv(), "CLASSPATH="+dexPath, "ANDROID_DATA="+c.dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("androidhelper: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("androidhelper: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("androidhelper: start %s: %w", runtime, err)
	}

	proc := &process{cmd: cmd, stdin: stdin, lines: make(chan string, 1)}
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 4096), 1<<20)
		for scanner.Scan() {
			proc.lines <- scanner.Text()
		}
		close(proc.lines)
	}()

	line, err := proc.readLine(startTimeout)
	if err == nil {
		if strategies, ok := strings.CutPrefix(line, "ready"); ok {
			proc.strategies = strings.TrimSpace(strategies)
		} else {
			err = fmt.Errorf("androidhelper: %s", strings.TrimPrefix(line, "error "))
		}
	}
	if err != nil {
		c.proc = proc
		c.stopLocked()
		return nil, err
	}
	return proc, nil
}

func (p *process) readLine(timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case line, ok := <-p.lines:
		if !ok {
			return "", errors.New("androidhelper: helper exited")
		}
		return line, nil
	case <-timer.C:
		return "", fmt.Errorf("androidhelper: no response within %s", timeout)
	}
}

// writeDex grava o dex com nome derivado do conteúdo e modo 0444: o ART do Android 14+
// aborta ao carregar dex gravável.
func (c *Client) writeDex(payload []byte) (string, error) {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return "", fmt.Errorf("androidhelper: create %q: %w", c.dir, err)
	}
	sum := sha256.Sum256(payload)
	path := filepath.Join(c.dir, "phonix-helper-"+hex.EncodeToString(sum[:6])+".dex")
	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(payload)) && info.Mode().Perm()&0o222 == 0 {
		return path, nil
	}
	tmp, err := os.CreateTemp(c.dir, ".helper-*.dex")
	if err != nil {
		return "", fmt.Errorf("androidhelper: create dex: %w", err)
	}
	_, writeErr := tmp.Write(payload)
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, closeErr, os.Chmod(tmp.Name(), 0o444)); err != nil {
		removeTemp(tmp.Name())
		return "", fmt.Errorf("androidhelper: write dex: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		removeTemp(tmp.Name())
		return "", fmt.Errorf("androidhelper: install dex: %w", err)
	}
	return path, nil
}

func removeTemp(path string) {
	if err := os.Remove(path); err != nil {
		slog.Debug("androidhelper: remove temporary dex", "path", path, "error", err)
	}
}

// filteredEnv remove variáveis que quebram o app_process sob o Termux (o termux-exec
// injeta LD_PRELOAD) e as que o helper redefine.
func filteredEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "LD_PRELOAD", "LD_LIBRARY_PATH", "CLASSPATH", "ANDROID_DATA":
			continue
		}
		env = append(env, kv)
	}
	return env
}
