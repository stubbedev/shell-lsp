package nixeval

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/shell-lsp/internal/atomicfile"
)

const (
	storeMagic     = "SLNX"
	storeVersion   = 1
	storeKeyOffset = len(storeMagic) + 4 + 4
	storeHeader    = storeKeyOffset + len(contentKey{})
	entrySuffix    = ".entry"
	latestSuffix   = ".latest"
	storeBudget    = 1 << 30
	cacheDirMode   = 0o700
)

var storeChecksum = crc32.MakeTable(crc32.Castagnoli)

type store struct {
	dir    string
	budget int64
	now    func() time.Time
	report func(error)
}

func (s *store) entryPath(key contentKey) string {
	return filepath.Join(s.dir, hex.EncodeToString(key[:])+entrySuffix)
}

func (s *store) latestPath(slot slotKey) string {
	return filepath.Join(s.dir, hex.EncodeToString(slot[:])+latestSuffix)
}

func frame(key contentKey, payload []byte) []byte {
	out := make([]byte, 0, storeHeader+len(payload))
	out = append(out, storeMagic...)
	out = binary.LittleEndian.AppendUint32(out, storeVersion)
	out = binary.LittleEndian.AppendUint32(out, crc32.Checksum(payload, storeChecksum))
	out = append(out, key[:]...)
	return append(out, payload...)
}

func unframe(key contentKey, data []byte) ([]byte, error) {
	if len(data) < storeHeader {
		return nil, fmt.Errorf("only %d bytes, shorter than a header", len(data))
	}
	if string(data[:len(storeMagic)]) != storeMagic {
		return nil, errors.New("it does not start with the cache magic")
	}
	if version := binary.LittleEndian.Uint32(data[len(storeMagic):]); version != storeVersion {
		return nil, fmt.Errorf("format %d, this build reads %d", version, storeVersion)
	}
	if !bytes.Equal(data[storeKeyOffset:storeHeader], key[:]) {
		return nil, errors.New("it holds the result of another key")
	}
	payload := data[storeHeader:]
	if sum := crc32.Checksum(payload, storeChecksum); sum != binary.LittleEndian.Uint32(data[len(storeMagic)+4:]) {
		return nil, errors.New("the checksum does not match")
	}
	return payload, nil
}

func (s *store) read(key contentKey) ([]byte, bool) {
	path := s.entryPath(key)
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is a hex digest inside the cache directory
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		s.report(fmt.Errorf("reading the evaluation cache entry %s: %w", path, err))
		return nil, false
	}
	payload, err := unframe(key, data)
	if err != nil {
		s.report(fmt.Errorf("discarding the evaluation cache entry %s: %w", path, err))
		return nil, false
	}
	now := s.now()
	if err := os.Chtimes(path, now, now); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.report(fmt.Errorf("marking the evaluation cache entry %s as used: %w", path, err))
	}
	return payload, true
}

func (s *store) write(key contentKey, payload []byte) {
	path := s.entryPath(key)
	if err := atomicfile.Write(path, frame(key, payload)); err != nil {
		s.report(fmt.Errorf("writing the evaluation cache entry: %w", err))
		return
	}
	if err := s.evict(filepath.Base(path)); err != nil {
		s.report(fmt.Errorf("evicting from the evaluation cache %s: %w", s.dir, err))
	}
}

func (s *store) latest(slot slotKey) (contentKey, bool) {
	path := s.latestPath(slot)
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is a hex digest inside the cache directory
	if errors.Is(err, fs.ErrNotExist) {
		return contentKey{}, false
	}
	if err != nil {
		s.report(fmt.Errorf("reading the evaluation cache pointer %s: %w", path, err))
		return contentKey{}, false
	}
	var key contentKey
	if len(data) != len(key) {
		s.report(fmt.Errorf("discarding the evaluation cache pointer %s: %d bytes, want %d", path, len(data), len(key)))
		return contentKey{}, false
	}
	copy(key[:], data)
	return key, true
}

func (s *store) setLatest(slot slotKey, key contentKey) {
	if slot == (slotKey{}) {
		return
	}
	if current, ok := s.latest(slot); ok && current == key {
		return
	}
	if err := atomicfile.Write(s.latestPath(slot), key[:]); err != nil {
		s.report(fmt.Errorf("writing the evaluation cache pointer: %w", err))
	}
}

type storedFile struct {
	name     string
	size     int64
	modified time.Time
}

func (s *store) evict(keep string) error {
	listed, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	files := make([]storedFile, 0, len(listed))
	var total int64
	for _, item := range listed {
		info, err := item.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		total += info.Size()
		files = append(files, storedFile{name: item.Name(), size: info.Size(), modified: info.ModTime()})
	}
	if total <= s.budget {
		return nil
	}
	slices.SortFunc(files, func(a, b storedFile) int {
		if order := a.modified.Compare(b.modified); order != 0 {
			return order
		}
		return strings.Compare(a.name, b.name)
	})
	var problems []error
	for _, file := range files {
		if total <= s.budget {
			break
		}
		if file.name == keep {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, file.name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			problems = append(problems, err)
			continue
		}
		total -= file.size
	}
	return errors.Join(problems...)
}
