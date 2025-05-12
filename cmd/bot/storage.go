package main

import (
	a "github.com/drypa/book-shelf/archive"
	"github.com/pkg/errors"
	"log/slog"
	"os"
	"path/filepath"
)

type storage struct {
	dir string
}

func newStorage(dir string) (*storage, error) {

	f, err := os.Stat(dir)
	if os.IsNotExist(err) {
		slog.Error("directory does not exist", "directory", dir)
		return nil, errors.New("directory does not exist")
	}
	if !f.IsDir() {
		slog.Error("path should be directory", "path", dir)
		return nil, errors.New("path should be directory")
	}
	return &storage{dir: dir}, nil
}
func (s *storage) GetFile(archive string, file string) ([]byte, error) {
	archivePath := filepath.Join(s.dir, archive)
	_, err := os.Stat(archivePath)
	if os.IsNotExist(err) {
		slog.Error("file does not exist", "archive", archive)
	}
	return a.UnzipFile(archivePath, file)
}
