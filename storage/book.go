package storage

import (
	"fmt"
	"regexp"
)

type Book struct {
	Id         int
	Title      string
	Authors    string
	Annotation string
	Genre      string
	Keywords   string
	Archive    string
	FileName   string
	FileSize   int64
}

func (b *Book) GetDownloadFileName() string {
	name := b.FileName
	title := b.Title
	return fmt.Sprintf("%s_%s", replaceInvalidFilenameChars(title), replaceInvalidFilenameChars(name))
}

func replaceInvalidFilenameChars(filename string) string {
	invalidCharPattern := regexp.MustCompile(`[\x00-\x1F<>:"/\\|?*\x7F]`)

	cleanedFilename := invalidCharPattern.ReplaceAllString(filename, "_")

	return cleanedFilename
}
