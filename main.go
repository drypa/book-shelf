package main

import (
	"book-shelf/scanner"
	"os"
)

func main() {
	s := scanner.NewScanner(os.Args[1])
	_ = s.Scan()
}
