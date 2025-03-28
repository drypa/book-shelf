package db

import (
	"context"
	"database/sql"
	"github.com/drypa/book-shelf/storage"
	"strings"
)

type Db struct {
	*sql.DB
}

func NewDb(DB *sql.DB) *Db {
	return &Db{DB: DB}
}
func (db *Db) Find(ctx context.Context, title *string, author *string) ([]*storage.Book, error) {

	var args []interface{}
	query := "SELECT id,title, authors,annotation,genre,keywords,archive,file_name,file_size from books"
	if author != nil || title != nil {
		query += " where"

		conditions := []string{}
		if author != nil {
			conditions = append(conditions, " lower(authors) like ?")
			args = append(args, "%"+strings.ToLower(*author)+"%")
		}
		if title != nil {
			conditions = append(conditions, " lower(title) like ?")
			args = append(args, "%"+strings.ToLower(*title)+"%")
		}
		joinedConditions := strings.Join(conditions, " and ")
		query += joinedConditions
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var books []*storage.Book
	for rows.Next() {
		b := storage.Book{}
		err := rows.Scan(&b.Id, &b.Title, &b.Authors, &b.Annotation, &b.Genre, &b.Keywords, &b.Archive, &b.FileName, &b.FileSize)
		if err != nil {
			return nil, err
		}
		books = append(books, &b)
	}
	return books, nil
}
