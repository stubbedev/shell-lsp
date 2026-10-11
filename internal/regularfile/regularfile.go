package regularfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

const configLimit = 1 << 20

var (
	ErrNotRegular = errors.New("not a regular file")
	ErrTooLarge   = errors.New("over the size limit")
)

func Stat(path string) (fs.FileInfo, error) {
	if platform.Native.IsDevicePath(path) {
		return nil, fmt.Errorf("%s names a device: %w", path, ErrNotRegular)
	}
	named, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !named.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is a %s: %w", path, named.Mode().Type(), ErrNotRegular)
	}
	return named, nil
}

func Open(path string) (*os.File, fs.FileInfo, error) {
	named, err := Stat(path)
	if err != nil {
		return nil, nil, err
	}
	return OpenStat(path, named)
}

func OpenStat(path string, named fs.FileInfo) (*os.File, fs.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|openFlags, 0) //nolint:gosec // callers pass paths they mean to read; this package only constrains what kind of file they get
	if err != nil {
		return nil, nil, err
	}
	opened, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("inspecting %s: %w", path, err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(named, opened) {
		_ = f.Close()
		return nil, nil, fmt.Errorf("%s changed into a %s while opening it: %w", path, opened.Mode().Type(), ErrNotRegular)
	}
	return f, opened, nil
}

func Read(path string, limit int64) ([]byte, fs.FileInfo, error) {
	named, err := Stat(path)
	if err != nil {
		return nil, nil, err
	}
	return ReadStat(path, named, limit)
}

func ReadStat(path string, named fs.FileInfo, limit int64) ([]byte, fs.FileInfo, error) {
	f, info, err := OpenStat(path, named)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	if info.Size() > limit {
		return nil, nil, fmt.Errorf("%s has %d bytes, %w of %d bytes", path, info.Size(), ErrTooLarge, limit)
	}
	data, err := ReadLimited(f, limit, info.Size())
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return data, info, nil
}

func ReadConfig(path string) ([]byte, error) {
	data, _, err := Read(path, configLimit)
	return data, err
}

func ReadLimited(r io.Reader, limit, sizeHint int64) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, min(sizeHint, limit)+bytes.MinRead))
	if _, err := buf.ReadFrom(io.LimitReader(r, limit+1)); err != nil {
		return nil, err
	}
	if int64(buf.Len()) > limit {
		return nil, fmt.Errorf("more than %d bytes: %w", limit, ErrTooLarge)
	}
	return buf.Bytes(), nil
}
