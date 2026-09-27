package database

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

const lockProbePattern = ".phonix_lock_probe-*"

func CheckPOSIXLockSupport(dirPath string) (supported bool, err error) {
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return false, fmt.Errorf("create lock probe directory %q: %w", dirPath, err)
	}

	probe, err := os.CreateTemp(dirPath, lockProbePattern)
	if err != nil {
		return false, fmt.Errorf("create lock probe in %q: %w", dirPath, err)
	}
	probePath := probe.Name()
	defer func() {
		if closeErr := probe.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close lock probe %q: %w", probePath, closeErr))
		}
		if removeErr := os.Remove(probePath); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove lock probe %q: %w", probePath, removeErr))
		}
	}()

	writeLock := syscall.Flock_t{
		Type:   syscall.F_WRLCK,
		Whence: int16(io.SeekStart),
		Start:  0,
		Len:    0,
	}
	if lockErr := syscall.FcntlFlock(probe.Fd(), syscall.F_SETLK, &writeLock); lockErr != nil {
		if unsupportedLockError(lockErr) {
			return false, nil
		}
		return false, fmt.Errorf("acquire lock probe %q: %w", probePath, lockErr)
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

func unsupportedLockError(err error) bool {
	return errors.Is(err, syscall.ENOSYS) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EINVAL)
}
