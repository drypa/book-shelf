package main

import (
	"fmt"
	s "github.com/drypa/book-shelf/storage"
)

type Search struct {
	Author  string
	Title   string
	Results []*s.Book
}

func (s *Search) UpdateAuthor(author string) {
	s.Author = author
}
func (s *Search) UpdateTitle(title string) {
	s.Title = title
}
func (s *Search) SetBooks(books []*s.Book) {
	s.Results = books
}

func (s *Search) GetResultsAsText() string {
	res := ""
	i := 1
	for _, book := range s.Results {
		res += fmt.Sprintf("%d. %s %s\n", i, book.Authors, book.Title)
		i++
	}
	return res
}

func (s *Search) GetBook(num int) *s.Book {
	if num < 1 || num > len(s.Results) {
		return nil
	}
	return s.Results[num-1]
}
