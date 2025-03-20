package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/drypa/book-shelf/book"
	"github.com/drypa/book-shelf/scanner"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	db, err := sql.Open("sqlite3", "./db.sqlite3")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer db.Close()

	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <directory>\n", os.Args[0])
		return
	}

	err = createTable(err, db)
	if err != nil {
		fmt.Println(err)
		return
	}

	files, err := scanner.GetFilesByMask(os.Args[1], ".zip.json")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, path := range files {
		fmt.Printf("processing %s\n", path)
		base := filepath.Base(path)
		zipName := strings.TrimSuffix(base, filepath.Ext(base))
		infos, err := readJson(path)
		if err != nil {
			fmt.Println(err)
			return
		}
		for _, info := range infos {
			if info != nil {
				err := insertBook(db, info, zipName)
				if err != nil {
					fmt.Println(err)
					return
				}
			}

		}

	}
}

func readJson(path string) ([]*book.Info, error) {
	fileData, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data []*book.Info
	err = json.Unmarshal(fileData, &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func createTable(err error, db *sql.DB) error {
	createTableSQL := `CREATE TABLE IF NOT EXISTS books (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT,
    authors TEXT,
    annotation TEXT,
    genre TEXT,
    keywords TEXT,
    archive TEXT,
    file_name TEXT,
    file_size BIGINT)`
	_, err = db.Exec(createTableSQL)
	return err
}

func insertBook(db *sql.DB, b *book.Info, archive string) error {
	insertQuery := `INSERT INTO books (title, authors,annotation,genre,keywords,archive,file_name,file_size)
	Values (?,?,?,?,?,?,?,?)`

	if b.Description == nil || b.TitleInfo == nil {
		fmt.Printf("file %s has no title\n", b.Filename)
		return nil
	}

	_, err := db.Exec(insertQuery,
		b.TitleInfo.BookTitle,
		b.TitleInfo.Authors(),
		b.Annotation,
		b.Genre,
		b.Keywords,
		archive,
		b.Filename,
		b.SizeInBytes,
	)
	return err
}
