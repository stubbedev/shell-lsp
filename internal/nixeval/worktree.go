package nixeval

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // G505: git names blobs by SHA-1, and matching those names lets an unchanged file skip being read
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/stubbedev/shell-lsp/internal/regularfile"
)

const (
	gitDirName        = ".git"
	maxIndexFile      = 256 << 20
	maxRefFile        = 1 << 16
	maxPackedRefsFile = 64 << 20
	maxTreeFiles      = 1 << 18
	maxTreeBytes      = 1 << 30
	gitlinkMode       = 0o160000
	regularModeMask   = 0o111
	kindAbsent        = "absent"
	kindExecutable    = "executable"
	kindFile          = "file"
	kindGitlink       = "gitlink"
	kindSymlink       = "symlink"
)

var errIndexUnsupported = errors.New("the git index has a form this reader leaves to a directory walk")

type treeDigest struct {
	root  string
	hash  hash.Hash
	files int
	bytes int64
}

func newTreeDigest(root, form string) *treeDigest {
	d := &treeDigest{root: root, hash: sha256.New()}
	writeField(d.hash, form)
	return d
}

func writeField(h hash.Hash, value string) {
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(value)))
	h.Write(length[:])
	h.Write([]byte(value))
}

func (d *treeDigest) add(path, kind string, id []byte) {
	writeField(d.hash, path)
	writeField(d.hash, kind)
	writeField(d.hash, string(id))
}

func (d *treeDigest) count(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fingerprinting the flake tree %s: %w", d.root, err)
	}
	d.files++
	if d.files > maxTreeFiles {
		return fmt.Errorf("fingerprinting the flake tree %s: more than %d files", d.root, maxTreeFiles)
	}
	return nil
}

func (d *treeDigest) fileID(path string, info fs.FileInfo) ([]byte, error) {
	d.bytes += info.Size()
	if d.bytes > maxTreeBytes {
		return nil, fmt.Errorf("fingerprinting the flake tree %s: more than %d bytes to read", d.root, maxTreeBytes)
	}
	file, opened, err := regularfile.OpenStat(path, info)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer file.Close()
	h := sha1.New() //nolint:gosec // G401: see the import
	fmt.Fprintf(h, "blob %d\x00", opened.Size())
	if _, err := io.Copy(h, file); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return h.Sum(nil), nil
}

func linkID(path string) ([]byte, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return nil, fmt.Errorf("reading the link %s: %w", path, err)
	}
	h := sha1.New() //nolint:gosec // G401: see the import
	fmt.Fprintf(h, "blob %d\x00%s", len(target), target)
	return h.Sum(nil), nil
}

func regularKind(info fs.FileInfo) string {
	if info.Mode().Perm()&regularModeMask != 0 {
		return kindExecutable
	}
	return kindFile
}

func fingerprintTree(ctx context.Context, dir string) (string, error) {
	repo, found, err := findRepository(dir)
	if err != nil {
		return "", err
	}
	if !found {
		return walkTree(ctx, dir)
	}
	head, err := repo.headState()
	if err != nil {
		return "", fmt.Errorf("reading the HEAD of %s: %w", repo.root, err)
	}
	relative, err := filepath.Rel(repo.root, dir)
	if err != nil {
		return "", fmt.Errorf("locating %s in %s: %w", dir, repo.root, err)
	}
	tracked, err := repo.trackedDigest(ctx)
	if errors.Is(err, errIndexUnsupported) {
		tracked, err = walkTree(ctx, repo.root)
	}
	if err != nil {
		return "", err
	}
	h := sha256.New()
	writeField(h, string(head))
	writeField(h, filepath.ToSlash(relative))
	writeField(h, tracked)
	return string(h.Sum(nil)), nil
}

func walkTree(ctx context.Context, root string) (string, error) {
	d := newTreeDigest(root, "walk")
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() {
			if item.Name() == gitDirName {
				return filepath.SkipDir
			}
			return nil
		}
		if err := d.count(ctx); err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return d.addWalked(path, filepath.ToSlash(relative), item)
	})
	if err != nil {
		return "", fmt.Errorf("walking the flake tree %s: %w", root, err)
	}
	return string(d.hash.Sum(nil)), nil
}

func (d *treeDigest) addWalked(path, relative string, item fs.DirEntry) error {
	info, err := item.Info()
	if errors.Is(err, fs.ErrNotExist) {
		d.add(relative, kindAbsent, nil)
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case info.Mode().IsRegular():
		id, err := d.fileID(path, info)
		if err != nil {
			return err
		}
		d.add(relative, regularKind(info), id)
	case info.Mode()&fs.ModeSymlink != 0:
		id, err := linkID(path)
		if err != nil {
			return err
		}
		d.add(relative, kindSymlink, id)
	default:
		d.add(relative, info.Mode().Type().String(), nil)
	}
	return nil
}

type repository struct {
	root   string
	gitDir string
	common string
}

func findRepository(dir string) (repository, bool, error) {
	for current := dir; ; current = filepath.Dir(current) {
		marker := filepath.Join(current, gitDirName)
		info, err := os.Stat(marker)
		switch {
		case err == nil && info.IsDir():
			return repository{root: current, gitDir: marker, common: marker}, true, nil
		case err == nil && info.Mode().IsRegular():
			repo, err := linkedRepository(current, marker)
			return repo, err == nil, err
		case err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR):
			return repository{}, false, fmt.Errorf("looking for a git repository at %s: %w", current, err)
		}
		if filepath.Dir(current) == current {
			return repository{}, false, nil
		}
	}
}

func linkedRepository(root, marker string) (repository, error) {
	data, _, err := regularfile.Read(marker, maxRefFile)
	if err != nil {
		return repository{}, fmt.Errorf("reading the git link %s: %w", marker, err)
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
	if !ok {
		return repository{}, fmt.Errorf("reading the git link %s: no gitdir line", marker)
	}
	gitDir := absoluteFrom(root, target)
	common := gitDir
	shared, _, err := regularfile.Read(filepath.Join(gitDir, "commondir"), maxRefFile)
	switch {
	case err == nil:
		common = absoluteFrom(gitDir, strings.TrimSpace(string(shared)))
	case !errors.Is(err, fs.ErrNotExist):
		return repository{}, fmt.Errorf("reading the common directory of %s: %w", gitDir, err)
	}
	return repository{root: root, gitDir: gitDir, common: common}, nil
}

func absoluteFrom(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}

func (r repository) headState() ([]byte, error) {
	head, _, err := regularfile.Read(filepath.Join(r.gitDir, "HEAD"), maxRefFile)
	if err != nil {
		return nil, err
	}
	name, symbolic := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: ")
	if !symbolic {
		return head, nil
	}
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return nil, fmt.Errorf("HEAD names the reference %q outside the repository", name)
	}
	tables, _, err := regularfile.Read(filepath.Join(r.common, "reftable", "tables.list"), maxRefFile)
	if err == nil {
		return append(head, tables...), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	loose, _, err := regularfile.Read(filepath.Join(r.common, filepath.FromSlash(name)), maxRefFile)
	if err == nil {
		return append(head, bytes.TrimSpace(loose)...), nil
	}
	if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
		return nil, err
	}
	packed, _, err := regularfile.Read(filepath.Join(r.common, "packed-refs"), maxPackedRefsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return head, nil
	}
	if err != nil {
		return nil, err
	}
	return append(head, packedReference(packed, name)...), nil
}

func packedReference(packed []byte, name string) []byte {
	suffix := []byte(" " + name)
	for line := range bytes.Lines(packed) {
		if id, found := bytes.CutSuffix(bytes.TrimRight(line, "\r\n"), suffix); found {
			return id
		}
	}
	return nil
}

func (r repository) trackedDigest(ctx context.Context) (string, error) {
	config, err := regularfile.ReadConfig(filepath.Join(r.common, "config"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("reading the git configuration of %s: %w", r.root, err)
	}
	if bytes.Contains(bytes.ToLower(config), []byte("objectformat")) {
		return "", errIndexUnsupported
	}
	d := newTreeDigest(r.root, "index")
	indexPath := filepath.Join(r.gitDir, "index")
	data, info, err := regularfile.Read(indexPath, maxIndexFile)
	if errors.Is(err, fs.ErrNotExist) {
		return string(d.hash.Sum(nil)), nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the git index of %s: %w", r.root, err)
	}
	err = eachIndexEntry(data, func(e indexEntry) error {
		if err := d.count(ctx); err != nil {
			return err
		}
		return d.addIndexed(e, info.ModTime())
	})
	if err != nil {
		return "", fmt.Errorf("reading the git index of %s: %w", r.root, err)
	}
	return string(d.hash.Sum(nil)), nil
}

func (d *treeDigest) addIndexed(e indexEntry, indexTime time.Time) error {
	if e.path == "" {
		return fmt.Errorf("%w: a split index", errIndexUnsupported)
	}
	if !filepath.IsLocal(filepath.FromSlash(e.path)) {
		return fmt.Errorf("it tracks the path %q outside the repository", e.path)
	}
	if e.mode == gitlinkMode {
		d.add(e.path, kindGitlink, e.id)
		return nil
	}
	path := filepath.Join(d.root, filepath.FromSlash(e.path))
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		d.add(e.path, kindAbsent, nil)
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case info.Mode().IsRegular():
		if e.unchanged(info, indexTime) {
			d.add(e.path, regularKind(info), e.id)
			return nil
		}
		id, err := d.fileID(path, info)
		if err != nil {
			return err
		}
		d.add(e.path, regularKind(info), id)
	case info.Mode()&fs.ModeSymlink != 0:
		id, err := linkID(path)
		if err != nil {
			return err
		}
		d.add(e.path, kindSymlink, id)
	default:
		d.add(e.path, info.Mode().Type().String(), nil)
	}
	return nil
}

type indexEntry struct {
	seconds     uint32
	nanoseconds uint32
	mode        uint32
	size        uint32
	id          []byte
	path        string
}

func (e indexEntry) unchanged(info fs.FileInfo, indexTime time.Time) bool {
	modified := info.ModTime()
	if uint32(info.Size()) != e.size || uint32(modified.Unix()) != e.seconds { //nolint:gosec // G115: the index itself stores these truncated to 32 bits
		return false
	}
	if e.nanoseconds != 0 && uint32(modified.Nanosecond()) != e.nanoseconds { //nolint:gosec // G115: a nanosecond count fits 32 bits
		return false
	}
	return modified.Before(indexTime)
}

const (
	indexHeaderSize   = 12
	indexStatSize     = 40
	indexIDSize       = 20
	indexFlagsSize    = 2
	indexFixedSize    = indexStatSize + indexIDSize + indexFlagsSize
	indexExtendedFlag = 0x4000
	indexExtendedSize = 2
	indexNameMask     = 0x0FFF
	indexExtensionHdr = 8
	indexAlignment    = 8
)

func eachIndexEntry(data []byte, visit func(indexEntry) error) error {
	if len(data) < indexHeaderSize || string(data[:4]) != "DIRC" {
		return errors.New("it does not start with an index header")
	}
	version := binary.BigEndian.Uint32(data[4:])
	if version < 2 || version > 4 {
		return fmt.Errorf("%w: version %d", errIndexUnsupported, version)
	}
	count := binary.BigEndian.Uint32(data[8:])
	pos := indexHeaderSize
	var previous string
	for range count {
		if len(data)-pos < indexFixedSize {
			return errors.New("an entry is cut short")
		}
		fixed := data[pos:]
		flags := binary.BigEndian.Uint16(fixed[indexStatSize+indexIDSize:])
		nameStart := pos + indexFixedSize
		if flags&indexExtendedFlag != 0 {
			nameStart += indexExtendedSize
		}
		if nameStart > len(data) {
			return errors.New("an entry is cut short")
		}
		var (
			path string
			next int
			err  error
		)
		if version == 4 {
			path, next, err = compressedName(data, nameStart, previous)
		} else {
			path, next, err = paddedName(data, pos, nameStart, int(flags&indexNameMask))
		}
		if err != nil {
			return err
		}
		e := indexEntry{
			seconds:     binary.BigEndian.Uint32(fixed[8:]),
			nanoseconds: binary.BigEndian.Uint32(fixed[12:]),
			mode:        binary.BigEndian.Uint32(fixed[24:]),
			size:        binary.BigEndian.Uint32(fixed[36:]),
			id:          fixed[indexStatSize : indexStatSize+indexIDSize],
			path:        path,
		}
		if err := visit(e); err != nil {
			return err
		}
		previous, pos = path, next
	}
	return checkIndexExtensions(data, pos)
}

func paddedName(data []byte, entryStart, nameStart, nameLength int) (string, int, error) {
	end := nameStart + nameLength
	if nameLength == indexNameMask {
		terminator := bytes.IndexByte(data[nameStart:], 0)
		if terminator < 0 {
			return "", 0, errors.New("a long path has no terminator")
		}
		end = nameStart + terminator
	}
	if end >= len(data) {
		return "", 0, errors.New("a path is cut short")
	}
	consumed := end - entryStart
	next := entryStart + (consumed+indexAlignment)&^(indexAlignment-1)
	if next > len(data) {
		return "", 0, errors.New("the padding of an entry is cut short")
	}
	return string(data[nameStart:end]), next, nil
}

func compressedName(data []byte, nameStart int, previous string) (string, int, error) {
	strip, width := indexVarint(data[nameStart:])
	if width == 0 || strip > uint64(len(previous)) {
		return "", 0, fmt.Errorf("a compressed path strips %d bytes of %d", strip, len(previous))
	}
	suffixStart := nameStart + width
	terminator := bytes.IndexByte(data[suffixStart:], 0)
	if terminator < 0 {
		return "", 0, errors.New("a compressed path has no terminator")
	}
	kept := previous[:len(previous)-int(strip)] //nolint:gosec // G115: strip is bounded by the length of previous
	return kept + string(data[suffixStart:suffixStart+terminator]), suffixStart + terminator + 1, nil
}

func indexVarint(data []byte) (uint64, int) {
	var value uint64
	for i, b := range data {
		if i > 0 {
			value++
		}
		value = value<<7 | uint64(b&0x7F)
		if b&0x80 == 0 {
			return value, i + 1
		}
		if i == binary.MaxVarintLen64 {
			return 0, 0
		}
	}
	return 0, 0
}

func checkIndexExtensions(data []byte, pos int) error {
	for len(data)-pos > indexIDSize {
		if len(data)-pos < indexExtensionHdr {
			return errors.New("an extension is cut short")
		}
		signature := string(data[pos : pos+4])
		if signature == "link" {
			return fmt.Errorf("%w: a split index", errIndexUnsupported)
		}
		size := uint64(binary.BigEndian.Uint32(data[pos+4:]))
		if size > uint64(len(data[pos+indexExtensionHdr:])) {
			return fmt.Errorf("the extension %q claims %d bytes", signature, size)
		}
		pos += indexExtensionHdr + int(size) //nolint:gosec // G115: size was bounded by the data
	}
	return nil
}
