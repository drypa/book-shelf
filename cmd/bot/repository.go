package main

import (
	"database/sql"
	"errors"
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
	query := "SELECT id, title, authors, annotation, genre, keywords, archive, file_name, file_size FROM books"
	conditions := []string{}
	args := []interface{}{}

	if title != "" {
		conditions = append(conditions, "title LIKE ? COLLATE NOCASE")
		args = append(args, "%"+title+"%")
	}
	if author != "" {
		conditions = append(conditions, "authors LIKE ? COLLATE NOCASE")
		args = append(args, "%"+author+"%")
	}
	if len(conditions) == 0 {
		return nil, errors.New("at least one search field (title or author) must be provided")
	}

	query = query + " WHERE " + strings.Join(conditions, " AND ") + " LIMIT 10;"

	rows, err := r.db.Query(query, args...)
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
