package main

import (
	"fmt"
	"github.com/drypa/book-shelf/scanner"
	"os"
	"strconv"
)

const ScanParallelismKey = "SCAN_PARALLELISM"
const defaultParallelism = 5

func main() {
	parallelism := defaultParallelism
	env := os.Getenv(ScanParallelismKey)
	if env != "" {
		res, err := strconv.Atoi(env)
		if err == nil {
			parallelism = res
		}
	}
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <directory>\n", os.Args[0])
		return
	}
	s := scanner.NewScanner(os.Args[1], parallelism)
	_ = s.Scan()
}
