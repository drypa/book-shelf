package book

import "github.com/drypa/book-shelf/format/fb2"

type Info struct {
	fb2.FictionBook `json:",inline"`
	SizeInBytes     int64  `json:"size_in_bytes"`
	Filename        string `json:"filename"`
}
