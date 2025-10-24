package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SaveRequest represents an artifact upload intent.
type SaveRequest struct {
	Version       string
	Artifact      io.Reader
	ArtifactName  string
	Signature     io.Reader
	SignatureName string
}

// Meta captures persisted artifact metadata.
type Meta struct {
	ArtifactName  string
	SignatureName string
	SHA256        string
	Size          int64
	CreatedAt     time.Time
	Path          string
	SignaturePath string
}

// Store provides persistence for upgrade artifacts.
type Store interface {
	Save(ctx context.Context, req SaveRequest) (Meta, error)
	Open(ctx context.Context, name string) (io.ReadSeekCloser, Meta, error)
}

// FileStore persists artifacts on the filesystem.
type FileStore struct {
	dir         string
	copyBufSize int
	bufferPool  sync.Pool
}

// NewFileStore constructs a FileStore rooted at dir.
func NewFileStore(dir string) (*FileStore, error) {
	return newFileStore(dir, defaultCopyBufferSize)
}

// NewFileStoreWithBuffer constructs a FileStore with a custom copy buffer size.
func NewFileStoreWithBuffer(dir string, copyBufferSize int) (*FileStore, error) {
	return newFileStore(dir, copyBufferSize)
}

func newFileStore(dir string, bufSize int) (*FileStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("artifact dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact dir: %w", err)
	}
	if bufSize <= 0 {
		bufSize = defaultCopyBufferSize
	}
	fs := &FileStore{
		dir:         dir,
		copyBufSize: bufSize,
	}
	fs.bufferPool = sync.Pool{
		New: func() any {
			return make([]byte, fs.copyBufSize)
		},
	}
	return fs, nil
}

// Save writes the artifact (and optional signature) to disk.
func (s *FileStore) Save(ctx context.Context, req SaveRequest) (Meta, error) {
	var meta Meta
	if req.Artifact == nil {
		return meta, fmt.Errorf("%w", ErrArtifactRequired)
	}
	now := time.Now().UTC()
	base := sanitizedBase(req.Version, req.ArtifactName)
	if base == "" {
		base = "artifact"
	}
	artifactExt := normalizedExt(req.ArtifactName)
	artifactName := fmt.Sprintf("%s-%d%s", base, now.Unix(), artifactExt)
	artifactPath := filepath.Join(s.dir, artifactName)
	tmpPath := artifactPath + ".tmp"

	buf := s.getCopyBuffer()
	defer s.putCopyBuffer(buf)

	file, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return meta, fmt.Errorf("create temp artifact: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	size, err := copyWithBuffer(io.MultiWriter(file, hasher), req.Artifact, buf)
	if err != nil {
		os.Remove(tmpPath)
		return meta, fmt.Errorf("write artifact: %w", err)
	}
	if err := file.Sync(); err != nil {
		os.Remove(tmpPath)
		return meta, fmt.Errorf("sync artifact: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return meta, fmt.Errorf("close artifact: %w", err)
	}
	if err := os.Rename(tmpPath, artifactPath); err != nil {
		os.Remove(tmpPath)
		return meta, fmt.Errorf("commit artifact: %w", err)
	}

	var signatureName, signaturePath string
	if req.Signature != nil {
		sigBase := sanitizedBase(req.Version, req.SignatureName)
		signatureName = buildSignatureName(sigBase, artifactName)
		signaturePath = filepath.Join(s.dir, signatureName)
		signTmp := signaturePath + ".tmp"
		sfile, err := os.OpenFile(signTmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return meta, fmt.Errorf("create signature: %w", err)
		}
		if _, err := copyWithBuffer(sfile, req.Signature, buf); err != nil {
			sfile.Close()
			os.Remove(signTmp)
			return meta, fmt.Errorf("write signature: %w", err)
		}
		if err := sfile.Close(); err != nil {
			os.Remove(signTmp)
			return meta, fmt.Errorf("close signature: %w", err)
		}
		if err := os.Rename(signTmp, signaturePath); err != nil {
			os.Remove(signTmp)
			return meta, fmt.Errorf("commit signature: %w", err)
		}
	}

	meta = Meta{
		ArtifactName:  artifactName,
		SignatureName: signatureName,
		SHA256:        hex.EncodeToString(hasher.Sum(nil)),
		Size:          size,
		CreatedAt:     now,
		Path:          artifactPath,
		SignaturePath: signaturePath,
	}
	return meta, nil
}

// Open returns a seekable reader for the stored artifact.
func (s *FileStore) Open(ctx context.Context, name string) (io.ReadSeekCloser, Meta, error) {
	var meta Meta
	if name == "" {
		return nil, meta, fmt.Errorf("%w", ErrArtifactNameRequired)
	}
	path := filepath.Join(s.dir, filepath.Clean(name))
	file, err := os.Open(path)
	if err != nil {
		return nil, meta, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, meta, err
	}
	meta.ArtifactName = name
	meta.Path = path
	meta.Size = info.Size()
	meta.CreatedAt = info.ModTime()
	return file, meta, nil
}

var sanitizeRegex = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizedBase(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		v = strings.TrimSuffix(v, filepath.Ext(v))
		v = sanitizeRegex.ReplaceAllString(v, "-")
		v = strings.Trim(v, "-_.")
		if v != "" {
			return v
		}
	}
	return ""
}

func normalizedExt(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return ".tar.gz"
	case strings.HasSuffix(lower, ".tar.xz"):
		return ".tar.xz"
	case strings.HasSuffix(lower, ".tar.bz2"):
		return ".tar.bz2"
	}
	ext := filepath.Ext(name)
	if ext == "" {
		return ".bin"
	}
	return ext
}

func buildSignatureName(base, artifactName string) string {
	if base != "" {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if base != "" {
		return base + ".sig"
	}
	if strings.HasSuffix(strings.ToLower(artifactName), ".sig") {
		return artifactName
	}
	return artifactName + ".sig"
}

// MemoryStore is an in-memory artifact store useful for tests.
type MemoryStore struct {
	mu       sync.Mutex
	files    map[string][]byte
	metadata map[string]Meta
}

// NewMemoryStore constructs a MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		files:    make(map[string][]byte),
		metadata: make(map[string]Meta),
	}
}

// Save stores artifact content in memory.
func (m *MemoryStore) Save(ctx context.Context, req SaveRequest) (Meta, error) {
	var meta Meta
	if req.Artifact == nil {
		return meta, fmt.Errorf("%w", ErrArtifactRequired)
	}

	base := sanitizedBase(req.Version, req.ArtifactName)
	if base == "" {
		base = "artifact"
	}
	artifactName := fmt.Sprintf("%s-%d%s", base, time.Now().UnixNano(), normalizedExt(req.ArtifactName))
	buf := &bytes.Buffer{}
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(buf, hasher), req.Artifact)
	if err != nil {
		return meta, err
	}
	var sigBuf []byte
	var signatureName string
	if req.Signature != nil {
		sigBuf, err = io.ReadAll(req.Signature)
		if err != nil {
			return meta, err
		}
		sigBase := sanitizedBase(req.Version, req.SignatureName)
		signatureName = buildSignatureName(sigBase, artifactName)
	}
	meta = Meta{
		ArtifactName:  artifactName,
		SignatureName: signatureName,
		SHA256:        hex.EncodeToString(hasher.Sum(nil)),
		Size:          size,
		CreatedAt:     time.Now().UTC(),
		Path:          artifactName,
		SignaturePath: signatureName,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[artifactName] = buf.Bytes()
	if signatureName != "" {
		m.files[signatureName] = sigBuf
	}
	m.metadata[artifactName] = meta
	return meta, nil
}

// Open retrieves artifact content from memory.
func (m *MemoryStore) Open(ctx context.Context, name string) (io.ReadSeekCloser, Meta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[name]
	if !ok {
		return nil, Meta{}, os.ErrNotExist
	}
	meta, ok := m.metadata[name]
	if !ok {
		meta = Meta{ArtifactName: name, CreatedAt: time.Now().UTC(), Size: int64(len(data))}
	}
	return ReadSeekNoopCloser{ReadSeeker: bytes.NewReader(data)}, meta, nil
}

// ReadSeekNoopCloser wraps an io.ReadSeeker with a no-op Close implementation.
type ReadSeekNoopCloser struct {
	io.ReadSeeker
}

// Close implements io.Closer for ReadSeekNoopCloser.
func (ReadSeekNoopCloser) Close() error { return nil }

const defaultCopyBufferSize = 512 * 1024

func (s *FileStore) getCopyBuffer() []byte {
	if s == nil {
		return make([]byte, defaultCopyBufferSize)
	}
	if v := s.bufferPool.Get(); v != nil {
		if buf, ok := v.([]byte); ok && len(buf) > 0 {
			return buf[:len(buf)]
		}
	}
	size := s.copyBufSize
	if size <= 0 {
		size = defaultCopyBufferSize
	}
	return make([]byte, size)
}

func (s *FileStore) putCopyBuffer(buf []byte) {
	if s == nil || buf == nil {
		return
	}
	if s.copyBufSize <= 0 {
		return
	}
	if len(buf) != s.copyBufSize {
		return
	}
	s.bufferPool.Put(buf)
}

func copyWithBuffer(dst io.Writer, src io.Reader, buf []byte) (int64, error) {
	if len(buf) == 0 {
		return io.Copy(dst, src)
	}
	return io.CopyBuffer(dst, src, buf)
}

var (
	// ErrArtifactRequired indicates no artifact payload was provided.
	ErrArtifactRequired = errors.New("artifact required")
	// ErrArtifactNameRequired indicates the caller did not specify an artifact name.
	ErrArtifactNameRequired = errors.New("artifact name required")
)
