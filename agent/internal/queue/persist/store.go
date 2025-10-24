package persist

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pingsantohq/agent/pkg/types"
)

const (
	segmentPrefix   = "segment-"
	segmentSuffix   = ".log"
	stateFileName   = "state.json"
	defaultMaxBytes = 2 << 30 // 2 GiB default if unspecified
)

type Store struct {
	mu          sync.Mutex
	dir         string
	maxBytes    int64
	segmentSize int64

	segments  []*segment
	writeSeg  *segment
	headState readerState

	totalSize int64
}

type segment struct {
	seq  int64
	path string
	file *os.File
	size int64
}

type readerState struct {
	Seq    int64 `json:"head_seq"`
	Offset int64 `json:"head_offset"`
}

type Batch struct {
	Results []types.ProbeResult
	entries []batchEntry
}

type batchEntry struct {
	seq   int64
	bytes int64
}

func Open(dir string, maxBytes, segmentSize int64) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("ensure spill dir %q: %w", dir, err)
	}

	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if segmentSize <= 0 || segmentSize > maxBytes {
		segmentSize = minInt64(maxBytes, 64<<20) // 64 MiB default segment
	}

	s := &Store{
		dir:         dir,
		maxBytes:    maxBytes,
		segmentSize: segmentSize,
	}

	if err := s.loadSegments(); err != nil {
		return nil, err
	}
	if err := s.loadState(); err != nil {
		return nil, err
	}
	if err := s.ensureWriteSegment(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) Append(result types.ProbeResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	record := make([]byte, 4+len(data))
	binary.BigEndian.PutUint32(record[:4], uint32(len(data)))
	copy(record[4:], data)

	if err := s.rotateIfNeeded(int64(len(record))); err != nil {
		return err
	}

	if _, err := s.writeSeg.file.Write(record); err != nil {
		return fmt.Errorf("write segment %q: %w", s.writeSeg.path, err)
	}
	if err := s.writeSeg.file.Sync(); err != nil {
		return fmt.Errorf("sync segment %q: %w", s.writeSeg.path, err)
	}
	s.writeSeg.size += int64(len(record))
	s.totalSize += int64(len(record))

	return s.enforceMaxBytes()
}

func (s *Store) ReadBatch(max int) (Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if max <= 0 {
		max = 1024
	}

	b := Batch{}

	if len(s.segments) == 0 {
		return b, nil
	}

	startSeq := s.headState.Seq
	if startSeq == 0 && len(s.segments) > 0 {
		startSeq = s.segments[0].seq
	}
	segIndex := s.segmentIndex(startSeq)
	if segIndex < 0 {
		segIndex = 0
		startSeq = s.segments[0].seq
		s.headState.Seq = startSeq
		s.headState.Offset = 0
	}

	var entries []batchEntry
	var results []types.ProbeResult
	offset := s.headState.Offset

	for segIndex < len(s.segments) && len(results) < max {
		seg := s.segments[segIndex]
		readOffset := offset
		if seg.seq != startSeq {
			readOffset = 0
		}

		file, err := os.OpenFile(seg.path, os.O_RDONLY, 0)
		if err != nil {
			return Batch{}, fmt.Errorf("open segment for read %q: %w", seg.path, err)
		}

		if _, err := file.Seek(readOffset, 0); err != nil {
			file.Close()
			return Batch{}, fmt.Errorf("seek segment %q: %w", seg.path, err)
		}

		for len(results) < max {
			lengthBuf := make([]byte, 4)
			if _, err := io.ReadFull(file, lengthBuf); err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					break
				}
				file.Close()
				return Batch{}, fmt.Errorf("read length: %w", err)
			}
			length := binary.BigEndian.Uint32(lengthBuf)
			payload := make([]byte, length)
			if _, err := io.ReadFull(file, payload); err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					break
				}
				file.Close()
				return Batch{}, fmt.Errorf("read payload: %w", err)
			}
			var result types.ProbeResult
			if err := json.Unmarshal(payload, &result); err != nil {
				file.Close()
				return Batch{}, fmt.Errorf("decode result: %w", err)
			}
			results = append(results, result)
			entries = append(entries, batchEntry{seq: seg.seq, bytes: int64(4 + length)})
			readOffset += int64(4 + length)
			if readOffset >= seg.size {
				break
			}
		}

		file.Close()

		if readOffset < seg.size {
			// remaining data in this segment; stop here.
			offset = readOffset
			break
		}

		// move to next segment
		offset = 0
		segIndex++
		startSeq = 0
	}

	b.Results = results
	b.entries = entries
	return b, nil
}

func (s *Store) Ack(batch Batch) error {
	if len(batch.entries) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range batch.entries {
		if s.headState.Seq != entry.seq {
			// We've moved to a new segment; ensure state matches.
			s.headState.Seq = entry.seq
			s.headState.Offset = 0
		}
		s.headState.Offset += entry.bytes

		seg := s.headSegment()
		if seg == nil {
			break
		}

		if s.headState.Offset >= seg.size {
			// Entire segment consumed.
			if err := os.Remove(seg.path); err != nil {
				return fmt.Errorf("remove segment %q: %w", seg.path, err)
			}
			s.totalSize -= seg.size
			s.removeHeadSegment()
			s.headState.Offset = 0
			if next := s.headSegment(); next != nil {
				s.headState.Seq = next.seq
			} else {
				s.headState.Seq = 0
			}
		}
	}

	return s.persistState()
}

func (s *Store) SizeBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.totalSize
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeSeg != nil && s.writeSeg.file != nil {
		if err := s.writeSeg.file.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) rotateIfNeeded(required int64) error {
	if s.writeSeg == nil {
		return s.createSegment(1)
	}
	if s.writeSeg.size+required <= s.segmentSize {
		return nil
	}
	return s.createSegment(s.writeSeg.seq + 1)
}

func (s *Store) createSegment(seq int64) error {
	if s.writeSeg != nil && s.writeSeg.file != nil {
		if err := s.writeSeg.file.Close(); err != nil {
			return fmt.Errorf("close segment: %w", err)
		}
	}
	path := filepath.Join(s.dir, fmt.Sprintf("%s%06d%s", segmentPrefix, seq, segmentSuffix))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("create segment %q: %w", path, err)
	}
	seg := &segment{
		seq:  seq,
		path: path,
		file: file,
		size: 0,
	}
	s.segments = append(s.segments, seg)
	sortSegments(s.segments)
	s.writeSeg = seg
	return nil
}

func (s *Store) ensureWriteSegment() error {
	if s.writeSeg != nil {
		return nil
	}
	if len(s.segments) == 0 {
		return s.createSegment(1)
	}
	last := s.segments[len(s.segments)-1]
	file, err := os.OpenFile(last.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open segment %q: %w", last.path, err)
	}
	last.file = file
	s.writeSeg = last
	return nil
}

func (s *Store) loadSegments() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("read spill dir: %w", err)
	}

	var segments []*segment
	var total int64

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, segmentPrefix) || !strings.HasSuffix(name, segmentSuffix) {
			continue
		}
		seqStr := strings.TrimSuffix(strings.TrimPrefix(name, segmentPrefix), segmentSuffix)
		seq, err := strconv.ParseInt(seqStr, 10, 64)
		if err != nil {
			continue
		}
		path := filepath.Join(s.dir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		segments = append(segments, &segment{
			seq:  seq,
			path: path,
			size: info.Size(),
		})
		total += info.Size()
	}

	sortSegments(segments)
	s.segments = segments
	s.totalSize = total
	return nil
}

func (s *Store) loadState() error {
	statePath := filepath.Join(s.dir, stateFileName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			if len(s.segments) > 0 {
				s.headState = readerState{
					Seq:    s.segments[0].seq,
					Offset: 0,
				}
			}
			return nil
		}
		return fmt.Errorf("read state file: %w", err)
	}
	var state readerState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parse state file: %w", err)
	}
	s.headState = state
	return nil
}

func (s *Store) persistState() error {
	statePath := filepath.Join(s.dir, stateFileName)
	data, err := json.Marshal(s.headState)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write state temp: %w", err)
	}
	if err := os.Rename(tmp, statePath); err != nil {
		return fmt.Errorf("commit state file: %w", err)
	}
	return nil
}

func (s *Store) headSegment() *segment {
	if len(s.segments) == 0 {
		return nil
	}
	return s.segments[0]
}

func (s *Store) removeHeadSegment() {
	if len(s.segments) == 0 {
		return
	}
	seg := s.segments[0]
	if seg == s.writeSeg {
		s.writeSeg = nil
	}
	s.segments = s.segments[1:]
}

func (s *Store) segmentIndex(seq int64) int {
	for i, seg := range s.segments {
		if seg.seq == seq {
			return i
		}
	}
	return -1
}

func (s *Store) enforceMaxBytes() error {
	for s.totalSize > s.maxBytes && len(s.segments) > 0 {
		seg := s.segments[0]
		if err := os.Remove(seg.path); err != nil {
			return fmt.Errorf("remove segment for max bytes %q: %w", seg.path, err)
		}
		s.totalSize -= seg.size
		s.removeHeadSegment()
		if s.headState.Seq == seg.seq {
			s.headState.Seq = 0
			s.headState.Offset = 0
		}
	}
	return s.persistState()
}

func sortSegments(segs []*segment) {
	sort.Slice(segs, func(i, j int) bool {
		return segs[i].seq < segs[j].seq
	})
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
