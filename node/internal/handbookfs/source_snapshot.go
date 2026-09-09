package handbookfs

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strings"
)

const (
	maxSourceSnapshotEntries = 10000
	maxSourceSnapshotBytes   = 8 << 20
)

// ReadSourceSnapshot reads the committed history entries attributed to source.
// It is strictly read-only: unlike History, it does not recover a pending
// transaction. A pending transaction makes the snapshot unconfirmable.
//
// The returned digest is SHA-256 of the canonical JSON encoding of the
// matching entries, and is therefore independent of entries from other
// maintenance sources. An empty matching snapshot has the digest of []
// (the JSON encoding of an empty entry list).
func (s *Service) ReadSourceSnapshot(ctx context.Context, source Provenance) ([]Entry, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	source = normalizedSnapshotProvenance(source)
	if source.MaintenanceReceiptID == "" || source.SessionID == "" || source.TurnID == "" {
		return nil, "", errors.New("complete provenance required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if _, err := os.Lstat(s.pendingPath()); err == nil {
		return nil, "", errors.New("handbook history has pending transaction")
	} else if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("check pending transaction: %w", err)
	}

	f, err := os.Open(s.manifest())
	if os.IsNotExist(err) {
		return emptySourceSnapshot()
	}
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	entries, err := readSourceManifest(ctx, f, source)
	if err != nil {
		return nil, "", err
	}
	if entries == nil {
		entries = []Entry{}
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return nil, "", fmt.Errorf("encode source snapshot: %w", err)
	}
	sum := sha256.Sum256(b)
	return entries, hex.EncodeToString(sum[:]), nil
}

func emptySourceSnapshot() ([]Entry, string, error) {
	b, _ := json.Marshal([]Entry{})
	sum := sha256.Sum256(b)
	return []Entry{}, hex.EncodeToString(sum[:]), nil
}

func normalizedSnapshotProvenance(p Provenance) Provenance {
	p.MaintenanceReceiptID = strings.TrimSpace(p.MaintenanceReceiptID)
	p.SessionID = strings.TrimSpace(p.SessionID)
	p.TurnID = strings.TrimSpace(p.TurnID)
	return p
}

func readSourceManifest(ctx context.Context, r io.Reader, source Provenance) ([]Entry, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	var entries []Entry
	var line []byte
	var total int
	var previousRevision int64
	var haveRevision bool
	var recordCount int
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		part, readErr := br.ReadSlice('\n')
		if len(part) != 0 {
			if len(part) > maxSourceSnapshotBytes-total {
				return nil, errors.New("handbook history exceeds snapshot byte limit")
			}
			total += len(part)
			line = append(line, part...)
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if len(line) != 0 {
			line = bytesTrimSpace(line)
			if len(line) != 0 {
				recordCount++
				if recordCount > maxSourceSnapshotEntries {
					return nil, errors.New("handbook history exceeds entry limit")
				}
				var e Entry
				if err := json.Unmarshal(line, &e); err != nil {
					return nil, fmt.Errorf("decode handbook history: %w", err)
				}
				if err := validateSnapshotEntry(e, previousRevision, haveRevision); err != nil {
					return nil, err
				}
				previousRevision, haveRevision = e.Revision, true
				if e.Provenance != nil && normalizedSnapshotProvenance(*e.Provenance) == source {
					entries = append(entries, e)
				}
			}
			line = line[:0]
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	return entries, nil
}

func bytesTrimSpace(b []byte) []byte {
	return bytes.TrimSpace(b)
}

func validateSnapshotEntry(e Entry, previous int64, havePrevious bool) error {
	if e.Revision <= 0 || (havePrevious && e.Revision <= previous) {
		return errors.New("handbook history revisions are not strictly increasing")
	}
	normalized := strings.ReplaceAll(e.Path, "\\", "/")
	clean := path.Clean(normalized)
	if normalized == "" || clean == "." || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(normalized, ":") {
		return errors.New("handbook history contains path outside namespace")
	}
	if !validSnapshotDigest(e.AfterDigest) || (e.BeforeDigest != "" && !validSnapshotDigest(e.BeforeDigest)) {
		return errors.New("handbook history contains invalid digest")
	}
	if e.CreatedAt.IsZero() {
		return errors.New("handbook history contains missing timestamp")
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".history" || (runtime.GOOS == "windows" && strings.EqualFold(part, ".history")) {
			return errors.New("handbook history contains private path")
		}
	}
	return nil
}

func validSnapshotDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
