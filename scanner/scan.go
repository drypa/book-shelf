package scanner

import (
	"archive/zip"
	"book-shelf/format/fb2"
	"fmt"
	"github.com/pkg/errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var parallelism = 5

type Scanner struct {
	directory string
}

func NewScanner(directory string) *Scanner {
	return &Scanner{directory: directory}
}

func (s *Scanner) Scan() error {

	files, err := getFilesByMask(s.directory, ".zip")
	if err != nil {
		return err
	}

	semaphore := make(chan struct{}, parallelism)

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
			}

		}(f, i)
	}
	wg.Wait()
	return nil
}

func processArchive(path string) error {
	fmt.Printf("Scanning %s\n", path)
	tempDir, err := os.MkdirTemp("", "")
	defer os.RemoveAll(tempDir)
	err = unzip(path, tempDir)
	if err != nil {
		return err
	}
	err = processFb2Books(tempDir)
	if err != nil {
		return err
	}
	return nil
}

func processFb2Books(path string) error {
	files, err := getFilesByMask(path, "fb2")
	if err != nil {
		return err
	}
	res := make([]fb2.FictionBook, len(files))
	for _, f := range files {
		metaInfo, err := fb2.ReadFb2(f)
		if err != nil {
			return err
		}
		res = append(res, *metaInfo)
		fmt.Printf("%s: %v", path, metaInfo)
	}
	//marshal, err := json.Marshal(res)
	return nil
}

func unzip(source string, destination string) error {
	zr, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		destPath := filepath.Join(destination, f.Name)
		if !strings.HasPrefix(destPath, filepath.Clean(destination)+string(os.PathSeparator)) {
			return fmt.Errorf("%s: illegal file path", destPath)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, os.ModePerm); err != nil {
				return errors.Wrapf(err, "%s: create directory", destPath)
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(destPath), os.ModePerm); err != nil {
			return errors.Wrapf(err, "%s: create directory", destPath)
		}
		outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		defer outFile.Close()
		if err != nil {
			return errors.Wrapf(err, "%s: open file", destPath)
		}
		rc, err := f.Open()
		defer rc.Close()
		if err != nil {
			return errors.Wrapf(err, "%s: open file", destPath)
		}
		_, err = io.Copy(outFile, rc)
		if err != nil {
			return errors.Wrapf(err, "%s: copy file", destPath)
		}
	}
	return nil
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
