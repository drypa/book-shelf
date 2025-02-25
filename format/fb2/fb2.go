package fb2

import (
	"encoding/xml"
	"fmt"
	"github.com/pkg/errors"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
	"io"
	"os"
	"strings"
)

type Description struct {
	XMLName   xml.Name  `xml:"description" json:"-"`
	TitleInfo TitleInfo `xml:"title-info" json:"title-info"`
	//Annotation string    `xml:"annotation" json:"annotation,omitempty"`
	Genre    string `xml:"genre" json:"genre,omitempty"`
	Keywords string `xml:"keywords" json:"keywords,omitempty"`
}

type TitleInfo struct {
	XMLName   xml.Name `xml:"title-info" json:"-"`
	BookTitle string   `xml:"book-title" json:"book-title"`
	Author    []Author `xml:"author" json:"author,omitempty"`
}

type Author struct {
	XMLName   xml.Name `xml:"author" json:"-"`
	FirstName string   `xml:"first-name" json:"firstname"`
	LastName  string   `xml:"last-name" json:"lastname"`
	NikName   string   `xml:"nickname" json:"nickname,omitempty"`
}

var charMap = map[string]*charmap.Charmap{
	"windows-1251": charmap.Windows1251,
	"windows-1252": charmap.Windows1252,
	"iso-8859-1":   charmap.ISO8859_1,
	"koi8-r":       charmap.KOI8R,
	"iso-8859-5":   charmap.ISO8859_5,
}

func ReadFb2(path string) (*Description, error) {
	reader, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decoder := xml.NewDecoder(reader)
	decoder.CharsetReader = func(encoding string, input io.Reader) (io.Reader, error) {
		currentCharMap := charMap[strings.ToLower(encoding)]
		if currentCharMap == nil {
			return nil, fmt.Errorf("unsupported encoding: %q", encoding)
		}

		return transform.NewReader(input, currentCharMap.NewDecoder()), nil
	}

	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("Error decoding XML:", err)
			return nil, errors.New("error decoding XML")
		}

		switch se := tok.(type) {
		case xml.StartElement:
			if se.Name.Local == "description" {
				var description Description
				if err := decoder.DecodeElement(&description, &se); err != nil {
					return nil, errors.Wrap(err, "error decoding description")
				}
				return &description, nil

			} else if se.Name.Local == "body" {
				_ = decoder.Skip()
			}
		}

		if err != nil {
			return nil, err
		}
	}
	return nil, errors.New("description could not be read")
}
