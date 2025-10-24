package upgrade

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pingsantohq/agent/internal/config"
)

// SignatureVerifier validates artifact signatures when provided.
type SignatureVerifier interface {
	Verify(ctx context.Context, artifactPath, signaturePath string) error
}

// ApplyResult captures metadata about an upgrade application attempt.
type ApplyResult struct {
	AppliedVersion  string
	PreviousVersion string
	AppliedAt       time.Time
	BundlePath      string
	ArtifactPath    string
}

// PlanApplier applies upgrade plans and returns the outcome.
type PlanApplier interface {
	Apply(ctx context.Context, plan Plan, state config.State) (ApplyResult, error)
}

// Applier downloads, verifies, and stages upgrade artifacts.
type Applier struct {
	DataDir    string
	HTTPClient *http.Client
	Verifier   SignatureVerifier
	Logger     *log.Logger
	Now        func() time.Time
}

// Apply performs the upgrade stages and returns the resulting metadata.
func (a *Applier) Apply(ctx context.Context, plan Plan, state config.State) (ApplyResult, error) {
	if a == nil {
		return ApplyResult{}, errors.New("applier not configured")
	}
	if strings.TrimSpace(a.DataDir) == "" {
		return ApplyResult{}, errors.New("data directory required")
	}
	if a.HTTPClient == nil {
		return ApplyResult{}, errors.New("http client required")
	}
	now := a.now().UTC()
	result := ApplyResult{
		AppliedVersion:  plan.Artifact.Version,
		PreviousVersion: state.Upgrade.Applied.Version,
		AppliedAt:       now,
	}

	bundleDir := filepath.Join(a.DataDir, "upgrades", plan.Artifact.Version)
	if err := os.RemoveAll(bundleDir); err != nil {
		return result, fmt.Errorf("clear bundle dir: %w", err)
	}
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		return result, fmt.Errorf("create bundle dir: %w", err)
	}

	artifactPath := filepath.Join(bundleDir, "artifact.tar.gz")
	if err := a.download(ctx, plan.Artifact.URL, artifactPath); err != nil {
		return result, err
	}
	if err := verifySHA256(artifactPath, plan.Artifact.SHA256); err != nil {
		return result, err
	}
	result.ArtifactPath = artifactPath

	if plan.Artifact.SignatureURL != "" {
		signaturePath := filepath.Join(bundleDir, "artifact.sig")
		if err := a.download(ctx, plan.Artifact.SignatureURL, signaturePath); err != nil {
			return result, err
		}
		if a.Verifier != nil {
			if err := a.Verifier.Verify(ctx, artifactPath, signaturePath); err != nil {
				return result, fmt.Errorf("verify signature: %w", err)
			}
		} else if a.Logger != nil {
			a.Logger.Printf("upgrade applier: signature verifier not configured; skipping verification")
		}
	}

	extractDir := filepath.Join(bundleDir, "bundle")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return result, fmt.Errorf("create extract dir: %w", err)
	}
	if err := extractTarGz(artifactPath, extractDir); err != nil {
		return result, err
	}

	result.BundlePath = extractDir
	return result, nil
}

func (a *Applier) download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", url, err)
	}
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: status %s", url, resp.Status)
	}

	tmp := dest + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("commit %s: %w", dest, err)
	}
	return nil
}

func verifySHA256(path string, expected string) error {
	if expected == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(strings.TrimSpace(expected), sum) {
		return fmt.Errorf("sha256 mismatch: expected %s got %s", strings.TrimSpace(expected), sum)
	}
	return nil
}

func extractTarGz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive %s: %w", archivePath, err)
	}
	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar header: %w", err)
		}

		targetPath := filepath.Join(destDir, filepath.Clean(header.Name))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("mkdir %s: %w", targetPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return fmt.Errorf("mkdir for file %s: %w", targetPath, err)
			}
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create file %s: %w", targetPath, err)
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("write file %s: %w", targetPath, err)
			}
			if err := outFile.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", targetPath, err)
			}
		default:
			// Skip other types for now.
			continue
		}
	}
	return nil
}

func (a *Applier) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
