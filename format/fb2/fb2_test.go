package fb2

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func _TestReadAllFb2(t *testing.T) {
	dir := "/home/drypa/Downloads/fb2-074392-091839/"
	files, _ := os.ReadDir(dir)
	for _, file := range files {
		fmt.Println(filepath.Join(dir, file.Name()))
		fb2, err := ReadFb2(filepath.Join(dir, file.Name()))
		if err != nil || fb2 == nil {
			t.Fatal(err, file.Name())
		}
	}
}

func _TestReadFb2(t *testing.T) {

	fb2, err := ReadFb2("/home/drypa/Downloads/fb2-074392-091839/91582.fb2")
	if err != nil {
		t.Fatal(err)
	}
	if fb2.TitleInfo.BookTitle == "" {
		t.Fatal(fb2.TitleInfo)
	}

}
