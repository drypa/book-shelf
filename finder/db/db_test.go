package db

import (
	"context"
	"database/sql"
	"github.com/drypa/book-shelf/storage"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"reflect"
	"testing"
)

func TestDb_Find(t *testing.T) {
	dbFile := "/tmp/test.db"
	_ = os.Remove(dbFile) // Удаляем файл, если он существует
	sqliteDB, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteDB.Close()
	defer os.Remove(dbFile)

	_, err = sqliteDB.Exec(`CREATE TABLE books (
		id INTEGER PRIMARY KEY,
		title TEXT,
		authors TEXT,
		annotation TEXT,
		genre TEXT,
		keywords TEXT,
		archive BOOLEAN,
		file_name TEXT,
		file_size INTEGER
	)`)
	if err != nil {
		t.Fatal(err)
	}

	database := NewDb(sqliteDB)
	expectedBook := storage.Book{
		Id:         1,
		Title:      "Test Title",
		Authors:    "Test Author",
		Annotation: "Some annotation",
		Genre:      "Fiction",
		Keywords:   "keyword1, keyword2",
		Archive:    "archive.zip",
		FileName:   "file.fb2",
		FileSize:   12345,
	}
	_, err = sqliteDB.Exec(`INSERT INTO books (title, authors, annotation, genre, keywords, archive, file_name, file_size) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		expectedBook.Title, expectedBook.Authors, expectedBook.Annotation, expectedBook.Genre, expectedBook.Keywords, expectedBook.Archive, expectedBook.FileName, expectedBook.FileSize)
	if err != nil {
		t.Fatal(err)
	}

	type args struct {
		ctx    context.Context
		title  *string
		author *string
	}
	tests := []struct {
		name    string
		args    args
		want    *storage.Book
		wantErr bool
	}{
		{
			name: "Full title",
			args: args{
				ctx:    context.Background(),
				title:  &expectedBook.Title,
				author: nil,
			},
			want:    &expectedBook,
			wantErr: false,
		},
		{
			name: "Full author",
			args: args{
				ctx:    context.Background(),
				title:  nil,
				author: &expectedBook.Authors,
			},
			want:    &expectedBook,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &Db{
				DB: database.DB,
			}
			got, err := db.Find(tt.args.ctx, tt.args.title, tt.args.author)
			if (err != nil) != tt.wantErr {
				t.Errorf("Find() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) < 1 {
				t.Errorf("Find() got = %v", got)
				return
			}

			if !reflect.DeepEqual(got[0], tt.want) {
				t.Errorf("Find() got = %v, want %v", got, tt.want)
			}
		})
	}
}
