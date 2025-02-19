package book

import "book-shelf/format/fb2"

//	type Author struct {
//		FirstName string `form:"first_name" json:"first_name"`
//		LastName  string `form:"last_name" json:"last_name"`
//	}
type BookInfo struct {
	fb2.FictionBook `sjon:",inline"`
	SizeInBytes     int64 `json:"size_in_bytes"`
}
