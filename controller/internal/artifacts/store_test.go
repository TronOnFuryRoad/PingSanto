package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreSaveStreamsLargeArtifacts(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewFileStoreWithBuffer(tmp, 8)
	if err != nil {
		t.Fatalf("NewFileStoreWithBuffer: %v", err)
	}

	payload := bytes.Repeat([]byte("stream-chunk-"), 1024)
	expectedHash := sha256.Sum256(payload)

	artifactMeta, err := store.Save(context.Background(), SaveRequest{
		Version:       "1.2.3",
		Artifact:      newChunkReader(payload, 5),
		ArtifactName:  "agent.tar.gz",
		Signature:     newChunkReader([]byte("signature"), 3),
		SignatureName: "agent.sig",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if artifactMeta.Size != int64(len(payload)) {
		t.Fatalf("unexpected size: got %d want %d", artifactMeta.Size, len(payload))
	}
	if artifactMeta.SHA256 != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("unexpected sha256: got %s", artifactMeta.SHA256)
	}
	data, err := os.ReadFile(artifactMeta.Path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("artifact content mismatch")
	}
	if artifactMeta.SignaturePath == "" {
		t.Fatalf("signature path missing")
	}
	signatureData, err := os.ReadFile(filepath.Join(tmp, filepath.Base(artifactMeta.SignaturePath)))
	if err != nil {
		t.Fatalf("read signature: %v", err)
	}
	if string(signatureData) != "signature" {
		t.Fatalf("signature content mismatch: %q", signatureData)
	}
}

type chunkReader struct {
	data      []byte
	chunkSize int
	offset    int
}

func newChunkReader(data []byte, chunk int) io.Reader {
	return &chunkReader{data: data, chunkSize: chunk}
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := r.chunkSize
	if n > len(p) {
		n = len(p)
	}
	if r.offset+n > len(r.data) {
		n = len(r.data) - r.offset
	}
	copy(p, r.data[r.offset:r.offset+n])
	r.offset += n
	return n, nil
}
