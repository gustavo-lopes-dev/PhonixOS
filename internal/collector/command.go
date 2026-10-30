package collector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// commandWaitDelay limita a espera pelos pipes após o cancelamento.
const commandWaitDelay = 500 * time.Millisecond

// commandContext monta um exec.Cmd com o caminho absoluto do utilitário,
// resolvido no PATH por os.Stat. Evita exec.LookPath: no Go 1.23 ele usa a
// syscall faccessat2, que o seccomp do Android pune com SIGSYS (o processo
// morre em vez de receber erro) em binários GOOS=linux.
// Utilitários como termux-battery-status são scripts cujo filho (termux-api)
// fica bloqueado sem o app Termux:API; ao expirar o contexto, todo o grupo de
// processos é encerrado e WaitDelay garante que Output retorne mesmo que um
// descendente ainda segure o pipe.
func commandContext(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	path, err := findExecutable(name, os.Getenv("PATH"))
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	killProcessGroupOnCancel(cmd)
	cmd.WaitDelay = commandWaitDelay
	return cmd, nil
}

func findExecutable(name, pathEnv string) (string, error) {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("collector: %q: %w", name, exec.ErrNotFound)
}
