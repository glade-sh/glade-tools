//go:build darwin || linux

package compat

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func configureLocalTestComparisonProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	return nil
}

func cleanupLocalTestComparisonProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	processGroup := -cmd.Process.Pid
	if err := syscall.Kill(processGroup, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill local test comparison process group: %w", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		gone, err := localTestComparisonProcessGroupGone(syscall.Kill(processGroup, 0))
		if gone {
			return nil
		}
		if err != nil {
			return fmt.Errorf("poll local test comparison process group: %w", err)
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for local test comparison process group cleanup")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func localTestComparisonProcessGroupGone(err error) (bool, error) {
	if errors.Is(err, syscall.ESRCH) {
		return true, nil
	}
	if err == nil || errors.Is(err, syscall.EPERM) {
		return false, nil
	}
	return false, err
}

func validateLocalTestComparisonOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("local test comparison file ownership is unavailable")
	}
	if stat.Uid != uint32(os.Getuid()) {
		return errors.New("local test comparison path is not owned by the current user")
	}
	return nil
}

func createLocalTestComparisonArtifactDirectory(parent *os.File, name string) (*os.File, error) {
	if err := unix.Mkdirat(int(parent.Fd()), name, 0o700); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR)
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func createLocalTestComparisonArtifactFile(directory *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func openLocalTestComparisonArtifactFile(directory *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func validateLocalTestComparisonArtifactDirectory(parent, directory *os.File, name string) error {
	var pathStat unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &pathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	var heldStat unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &heldStat); err != nil {
		return err
	}
	if pathStat.Dev != heldStat.Dev || pathStat.Ino != heldStat.Ino {
		return errors.New("local test comparison artifact directory identity changed")
	}
	return nil
}

func removeLocalTestComparisonArtifactDirectory(parent, directory *os.File, name string, files []string) error {
	var removeErr error
	for _, file := range files {
		if err := unix.Unlinkat(int(directory.Fd()), file, 0); err != nil && !errors.Is(err, syscall.ENOENT) {
			removeErr = errors.Join(removeErr, err)
		}
	}
	if err := directory.Close(); err != nil {
		removeErr = errors.Join(removeErr, err)
	}
	if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, syscall.ENOENT) {
		removeErr = errors.Join(removeErr, err)
	}
	return removeErr
}
