package db

type Query struct {
	title  string
	author string
}
type FindResult struct {
	Num      int
	Title    string
	Author   string
	FileSize int64
	FileName string
}
