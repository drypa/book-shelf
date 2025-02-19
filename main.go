package main

import (
	"book-shelf/scanner"
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
	s := scanner.NewScanner(os.Args[1], parallelism)
	_ = s.Scan()
}
