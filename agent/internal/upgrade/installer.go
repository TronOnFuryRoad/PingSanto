package upgrade

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// InstallResult captures metadata about an installation attempt.
type InstallResult struct {
	TargetPath string
	BackupPath string
}

// Installer installs the staged binary into the desired location.
type Installer interface {
	Install(ctx context.Context, sourcePath string) (InstallResult, error)
	Rollback(ctx context.Context, res InstallResult) error
}

// BinaryInstaller replaces the current executable with the staged binary.
type BinaryInstaller struct {
	TargetPath string
	Logger     *log.Logger
}

// Install copies sourcePath over the target executable, creating a backup for rollback.
func (i *BinaryInstaller) Install(ctx context.Context, sourcePath string) (InstallResult, error) {
	var result InstallResult

	target, err := i.targetExecutable()
	if err != nil {
		return result, err
	}
	if sourcePath == "" {
		return result, fmt.Errorf("source path required")
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return result, fmt.Errorf("stat source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return result, fmt.Errorf("source %q is not a regular file", sourcePath)
	}

	backup := target + ".bak"
	temp := target + ".tmp"

	if err := os.Remove(temp); err != nil && !os.IsNotExist(err) {
		return result, fmt.Errorf("remove temp: %w", err)
	}

	targetInfo, err := os.Stat(target)
	var targetMode os.FileMode = 0o755
	if err == nil {
		targetMode = targetInfo.Mode()
		if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
			return result, fmt.Errorf("remove backup: %w", err)
		}
		if err := os.Rename(target, backup); err != nil {
			return result, fmt.Errorf("backup current binary: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return result, fmt.Errorf("stat target: %w", err)
	}

	if err := copyFile(sourcePath, temp, info.Mode()); err != nil {
		// attempt to restore backup if copy fails
		os.Remove(temp)
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = os.Rename(backup, target)
		}
		return result, err
	}

	if err := os.Chmod(temp, targetMode); err != nil {
		os.Remove(temp)
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = os.Rename(backup, target)
		}
		return result, fmt.Errorf("chmod temp binary: %w", err)
	}

	if err := os.Rename(temp, target); err != nil {
		os.Remove(temp)
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = os.Rename(backup, target)
		}
		return result, fmt.Errorf("publish binary: %w", err)
	}

	if i.Logger != nil {
		i.Logger.Printf("upgrade installer: installed %s (backup=%s)", target, backup)
	}

	result.TargetPath = target
	result.BackupPath = backup
	return result, nil
}

// Rollback restores the backup created during Install.
func (i *BinaryInstaller) Rollback(ctx context.Context, res InstallResult) error {
	if res.BackupPath == "" || res.TargetPath == "" {
		return nil
	}
	if _, err := os.Stat(res.BackupPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if i.Logger != nil {
		i.Logger.Printf("upgrade installer: rolling back to %s", res.BackupPath)
	}
	if err := os.Rename(res.BackupPath, res.TargetPath); err != nil {
		return fmt.Errorf("rollback rename: %w", err)
	}
	return nil
}

func (i *BinaryInstaller) targetExecutable() (string, error) {
	if strings.TrimSpace(i.TargetPath) != "" {
		return i.TargetPath, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("determine executable: %w", err)
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	return real, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("open dest: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy file: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync file: %w", err)
	}
	return nil
}
