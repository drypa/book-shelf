package fb2

import (
	"encoding/xml"
	"fmt"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
	"io"
	"os"
)

type FictionBook struct {
	XMLName     xml.Name    `xml:"FictionBook"`
	Description Description `xml:"description" json:",inline"`
}
type Description struct {
	XMLName    xml.Name  `xml:"description"`
	TitleInfo  TitleInfo `xml:"title-info" json:"title-info"`
	Annotation string    `xml:"annotation" json:"annotation"`
	Genre      string    `xml:"genre" json:"genre"`
	Keywords   string    `xml:"keywords" json:"keywords"`
}

type TitleInfo struct {
	XMLName   xml.Name `xml:"title-info"`
	BookTitle string   `xml:"book-title" json:"book-title"`
	Author    []Author `xml:"author" json:"author"`
}

type Author struct {
	XMLName   xml.Name `xml:"author"`
	FirstName string   `xml:"first-name" json:"firstname"`
	LastName  string   `xml:"last-name" xml:"lastname"`
	NikName   string   `xml:"nickname" xml:"nickname"`
}

func ReadFb2(path string) (*FictionBook, error) {
	reader, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decoder := xml.NewDecoder(reader)
	decoder.CharsetReader = func(encoding string, input io.Reader) (io.Reader, error) {
		if encoding == "windows-1251" {
			return transform.NewReader(input, charmap.Windows1251.NewDecoder()), nil
		}
		return nil, fmt.Errorf("unsupported encoding: %q", encoding)
	}

	fb := FictionBook{}
	err = decoder.Decode(&fb)
	if err != nil {
		return nil, err
	}
	return &fb, nil
}
