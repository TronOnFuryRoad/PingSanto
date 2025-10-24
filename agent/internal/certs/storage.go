package certs

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	Cert string
	Key  string
	CA   string
}

func Persist(paths Paths, resp *Response) error {
	if resp == nil {
		return nil
	}

	if len(resp.CertPEM) > 0 {
		if err := writeFile(paths.Cert, resp.CertPEM); err != nil {
			return err
		}
	}
	if len(resp.KeyPEM) > 0 {
		if err := writeFile(paths.Key, resp.KeyPEM); err != nil {
			return err
		}
	}
	if len(resp.CAPEM) > 0 {
		if err := writeFile(paths.CA, resp.CAPEM); err != nil {
			return err
		}
	}

	return nil
}

func writeFile(path string, data []byte) error {
	if path == "" {
		return fmt.Errorf("persist certificate: path is empty")
	}
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("ensure directory for %q: %w", path, err)
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("persist certificate at %q: %w", path, err)
	}
	return nil
}
