package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func WriteSignedConfig(path string, data []byte) error {
	if len(data) == 0 {
		return nil
	}

	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("ensure config dir %q: %w", dir, err)
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return fmt.Errorf("write temp config %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit config %q: %w", path, err)
	}

	return nil
}
