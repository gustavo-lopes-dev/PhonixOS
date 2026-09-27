package database

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const lockProbeFileName = ".phonix_lock_probe"

func CheckPOSIXLockSupport(dirPath string) (supported bool, err error) {
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return false, fmt.Errorf("create lock probe directory %q: %w", dirPath, err)
	}

	probePath := filepath.Join(dirPath, lockProbeFileName)
	probe, err := os.OpenFile(probePath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return false, fmt.Errorf("open lock probe %q: %w", probePath, err)
	}
	defer func() {
		if closeErr := probe.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close lock probe %q: %w", probePath, closeErr)
		}
		if removeErr := os.Remove(probePath); removeErr != nil && err == nil {
			err = fmt.Errorf("remove lock probe %q: %w", probePath, removeErr)
		}
	}()

	writeLock := syscall.Flock_t{
		Type:   syscall.F_WRLCK,
		Whence: int16(io.SeekStart),
		Start:  0,
		Len:    0,
	}
	if lockErr := syscall.FcntlFlock(probe.Fd(), syscall.F_SETLK, &writeLock); lockErr != nil {
		return false, nil
	}

	unlock := syscall.Flock_t{
		Type:   syscall.F_UNLCK,
		Whence: int16(io.SeekStart),
		Start:  0,
		Len:    0,
	}
	if unlockErr := syscall.FcntlFlock(probe.Fd(), syscall.F_SETLK, &unlock); unlockErr != nil {
		return false, fmt.Errorf("release lock probe %q: %w", probePath, unlockErr)
	}

	return true, nil
}
