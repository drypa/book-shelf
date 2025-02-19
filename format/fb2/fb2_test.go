package fb2

import (
	"testing"
)

func IgnoreTestReadFb2(t *testing.T) {

	fb2, err := ReadFb2("/home/drypa/Downloads/fb2-030560-060423/30569.fb2")
	if err != nil {
		t.Fatal(err)
	}
	if fb2.Description.TitleInfo.BookTitle == "" {
		t.Fatal(fb2.Description.TitleInfo)
	}
}
