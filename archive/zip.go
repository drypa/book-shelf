package archive

import (
	"archive/zip"
	"fmt"
	"github.com/pkg/errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Unzip(source string, destination string) error {
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
