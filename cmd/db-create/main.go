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
		err = insertBooks(db, infos, zipName)
		if err != nil {
			fmt.Println(err)
			return
		}
	}

}

func insertBooks(db *sql.DB, infos []*book.Info, zipName string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	insertQuery := `INSERT INTO books (title, authors,annotation,genre,keywords,archive,file_name,file_size)
	Values (?,?,?,?,?,?,?,?)`
	stmt, err := tx.Prepare(insertQuery)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, b := range infos {
		if b != nil {
			if b.Description == nil || b.TitleInfo == nil {
				fmt.Printf("file %s has no title\n", b.Filename)
				continue
			}
			_, err := stmt.Exec(
				b.TitleInfo.BookTitle,
				b.TitleInfo.Authors(),
				b.Annotation,
				b.Genre,
				b.Keywords,
				zipName,
				b.Filename,
				b.SizeInBytes,
			)

			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
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
