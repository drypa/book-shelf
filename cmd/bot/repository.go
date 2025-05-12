package main

import (
	"database/sql"
	s "github.com/drypa/book-shelf/storage"
	"strings"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Search(title string, author string) ([]*s.Book, error) {
	query := "SELECT id, title, authors, annotation, genre, keywords, archive, file_name, file_size FROM books WHERE lower(authors) like ? and lower(title) like ? LIMIT 100;"
	authorPattern := "%" + strings.ToLower(author) + "%"
	titlePattern := "%" + strings.ToLower(title) + "%"

	rows, err := r.db.Query(query, authorPattern, titlePattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]*s.Book, 0)

	for rows.Next() {
		book := &s.Book{}
		if err := rows.Scan(&book.Id, &book.Title, &book.Authors, &book.Annotation, &book.Genre, &book.Keywords, &book.Archive, &book.FileName, &book.FileSize); err != nil {
			return nil, err
		}
		result = append(result, book)
	}
	return result, nil
}
