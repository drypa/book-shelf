package scanner

import (
	"encoding/json"
	"fmt"
	"github.com/drypa/book-shelf/archive"
	"github.com/drypa/book-shelf/book"
	"github.com/drypa/book-shelf/format/fb2"
	"github.com/pkg/errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Scanner struct {
	directory   string
	parallelism int
}

func NewScanner(directory string, parallelism int) *Scanner {
	return &Scanner{directory: directory, parallelism: parallelism}
}

func (s *Scanner) Scan() error {

	files, err := getFilesByMask(s.directory, ".zip")
	if err != nil {
		return err
	}

	semaphore := make(chan struct{}, s.parallelism)

	wg := sync.WaitGroup{}
	for i, f := range files {
		semaphore <- struct{}{}
		wg.Add(1)
		go func(name string, i int) {
			defer wg.Done()
			defer func() { <-semaphore }()
			err = processArchive(name)
			if err != nil {
				log.Printf("failed to process archive %s: %s", name, err)
			} else {
				log.Printf("processing archive %s", name)
			}

		}(f, i)
	}
	wg.Wait()
	return nil
}

func processArchive(path string) error {
	log.Printf("Scanning %s\n", path)
	tempDir, err := os.MkdirTemp("", "bookshelf")
	defer os.RemoveAll(tempDir)
	err = archive.Unzip(path, tempDir)
	if err != nil {
		return err
	}
	metadata, err := processFb2Books(tempDir)
	if err != nil {
		return err
	}
	metadataPath := fmt.Sprintf("%s.%s", path, "json")
	marshal, err := json.Marshal(metadata)
	if err != nil {
		return errors.Wrap(err, "failed to marshal metadata")
	}
	err = os.WriteFile(metadataPath, marshal, 0644)
	if err != nil {
		return errors.Wrapf(err, "failed to write metadata to %s", metadataPath)
	}
	return nil
}

func processFb2Books(path string) ([]book.Info, error) {
	files, err := getFilesByMask(path, "fb2")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get fb2 files")
	}
	res := make([]book.Info, len(files))
	for i, f := range files {
		info, err := readFb2Meta(f)
		if err != nil {
			log.Printf("failed to read metadata from %s: %s", f, err)
			continue
		}
		res[i] = *info
	}
	return res, nil
}

func readFb2Meta(f string) (*book.Info, error) {
	metaInfo, err := fb2.ReadFb2(f)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read fb2 file %s", f)
	}
	stat, err := os.Stat(f)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to stat file %s", f)
	}
	info := book.Info{
		FictionBook: *metaInfo,
		SizeInBytes: stat.Size(),
		Filename:    filepath.Base(f),
	}
	return &info, nil
}

func getFilesByMask(directory string, suffix string) ([]string, error) {
	files, err := os.ReadDir(directory)
	var res []string
	if err != nil {
		return res, err
	}
	for _, f := range files {
		if f.IsDir() {
			filesByMask, err := getFilesByMask(filepath.Join(directory, f.Name()), suffix)
			if err != nil {
				return res, err
			}
			res = append(res, filesByMask...)
			continue
		}
		if strings.HasSuffix(f.Name(), suffix) {
			res = append(res, filepath.Join(directory, f.Name()))
		}
	}
	return res, nil
}
