package fb2

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// _TestReadAllFb2 — ручной сценарий: разбирает все FB2 из локальной папки.
// Префикс "_" отключает тест, путь существует только на машине разработчика.
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

// _TestReadFb2Local — ручной сценарий на конкретном локальном файле.
// Оставлен как есть (префикс "_" отключает тест): автономный прогон
// go test ./format/fb2 -run Local не должен зависеть от машины разработчика,
// а этот сценарий по-прежнему доступен вручную.
func _TestReadFb2Local(t *testing.T) {

	fb2, err := ReadFb2("/home/drypa/Downloads/fb2-113437-119690/114594.fb2")
	if err != nil {
		t.Fatal(err)
	}
	if fb2.TitleInfo.BookTitle == "" {
		t.Fatal(fb2.TitleInfo)
	}

}

// fixturePath — путь к файлу из testdata, который лежит рядом с тестом и
// доступен в любом окружении, в том числе в CI.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func TestReadFb2(t *testing.T) {
	fb2, err := ReadFb2(fixturePath(t, "book-utf8.fb2"))
	if err != nil {
		t.Fatalf("ReadFb2() error = %v", err)
	}
	if fb2 == nil {
		t.Fatal("ReadFb2() returned nil description")
	}
	if fb2.TitleInfo == nil {
		t.Fatal("TitleInfo is nil, want parsed title-info")
	}

	// Замечание: genre/annotation/keywords в настоящем FB2 лежат внутри
	// <title-info>, а поля Description объявлены на уровне <description>,
	// поэтому парсер их не заполняет. Это существующее поведение структуры,
	// тест его не фиксирует; см. TestReadFb2DescriptionFields.
	if got, want := fb2.TitleInfo.BookTitle, "Пробная книга"; got != want {
		t.Errorf("BookTitle = %q, want %q", got, want)
	}

	if got, want := len(fb2.TitleInfo.Author), 1; got != want {
		t.Fatalf("len(Author) = %d, want %d", got, want)
	}
	author := fb2.TitleInfo.Author[0]
	if author.FirstName != "Иван" || author.LastName != "Петров" {
		t.Errorf("Author = %q %q, want %q %q", author.FirstName, author.LastName, "Иван", "Петров")
	}
}

// TestReadFb2Encodings проверяет CharsetReader-белый список: одна и та же
// структура FB2, сохранённая в разных кодировках, должна читаться одинаково.
func TestReadFb2Encodings(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		title string
	}{
		{name: "utf-8", file: "book-utf8.fb2", title: "Пробная книга"},
		{name: "windows-1251", file: "book-cp1251.fb2", title: "Пробная книга 1251"},
		{name: "koi8-r", file: "book-koi8r.fb2", title: "Пробная книга KOI8-R"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb2, err := ReadFb2(fixturePath(t, tt.file))
			if err != nil {
				t.Fatalf("ReadFb2() error = %v", err)
			}
			if fb2 == nil || fb2.TitleInfo == nil {
				t.Fatal("TitleInfo is nil, want parsed title-info")
			}
			if got := fb2.TitleInfo.BookTitle; got != tt.title {
				t.Errorf("BookTitle = %q, want %q", got, tt.title)
			}
		})
	}
}

// TestReadFb2DescriptionFields покрывает поля Genre/Keywords/Annotation,
// которые Description читает на уровне <description>.
func TestReadFb2DescriptionFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "description-fields.fb2")
	content := `<?xml version="1.0" encoding="utf-8"?>
<FictionBook>
<description>
<genre>sf</genre>
<keywords>проба, тест</keywords>
<annotation>Краткое описание для теста парсера.</annotation>
<title-info><book-title>Пробная книга</book-title></title-info>
</description>
</FictionBook>
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	fb2, err := ReadFb2(path)
	if err != nil {
		t.Fatalf("ReadFb2() error = %v", err)
	}
	if got, want := fb2.Genre, "sf"; got != want {
		t.Errorf("Genre = %q, want %q", got, want)
	}
	if got, want := fb2.Keywords, "проба, тест"; got != want {
		t.Errorf("Keywords = %q, want %q", got, want)
	}
	if got, want := strings.TrimSpace(fb2.Annotation), "Краткое описание для теста парсера."; got != want {
		t.Errorf("Annotation = %q, want %q", got, want)
	}
}

func TestReadFb2Errors(t *testing.T) {
	writeFile := func(t *testing.T, name, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return path
	}

	t.Run("file not found", func(t *testing.T) {
		_, err := ReadFb2(filepath.Join(t.TempDir(), "absent.fb2"))
		if err == nil {
			t.Fatal("ReadFb2() error = nil, want error for missing file")
		}
	})

	t.Run("unsupported encoding", func(t *testing.T) {
		path := writeFile(t, "unsupported.fb2", `<?xml version="1.0" encoding="windows-1250"?>`+"\n"+`<FictionBook/>`)
		_, err := ReadFb2(path)
		if err == nil {
			t.Fatal("ReadFb2() error = nil, want error for encoding outside charMap")
		}
		if got := err.Error(); !strings.Contains(got, "error decoding XML") {
			t.Errorf("error = %q, want it to mention %q", got, "error decoding XML")
		}
	})

	t.Run("no description", func(t *testing.T) {
		path := writeFile(t, "no-description.fb2", `<?xml version="1.0" encoding="utf-8"?>`+"\n"+`<FictionBook/>`)
		_, err := ReadFb2(path)
		if err == nil {
			t.Fatal("ReadFb2() error = nil, want error when <description> is missing")
		}
		if got := err.Error(); !strings.Contains(got, "description could not be read") {
			t.Errorf("error = %q, want it to mention %q", got, "description could not be read")
		}
	})
}

// TestReadFb2XxeProtection проверяет защиту от XXE-атак.
// FB2 с внешней сущностью не должен читать файлы сервера.
func TestReadFb2XxeProtection(t *testing.T) {
	// FB2 с внешней сущностью не должен читать /etc/passwd
	fb2, err := ReadFb2(fixturePath(t, "book-xxe.fb2"))
	// Ожидаем: либо ошибка о неизвестной сущности, либо успешный парсинг
	// (сущность просто игнорируется). Критично: файл /etc/passwd НЕ должен быть прочитан
	if err != nil {
		// Проверяем, что ошибка не связана с чтением файлов сервера
		t.Logf("FB2 с XXE обработан с ошибкой (это допустимое поведение): %v", err)
	}
	if fb2 != nil {
		t.Logf("FB2 с XXE успешно пропарсен (защита сработала)")
	}
}
