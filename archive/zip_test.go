package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestUnzipFile(t *testing.T) {
	zipFile := "./testdata/files.zip"
	type args struct {
		zipPath  string
		fileName string
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name:    "Extract single file",
			args:    args{zipPath: zipFile, fileName: "1.txt"},
			want:    []byte("1.txt\n"),
			wantErr: false,
		},
		{
			name:    "Extract single file",
			args:    args{zipPath: zipFile, fileName: "2.txt"},
			want:    []byte("2.txt\n"),
			wantErr: false,
		},
		{
			name:    "Extract single file",
			args:    args{zipPath: zipFile, fileName: "3.txt"},
			want:    []byte("3.txt\n"),
			wantErr: false,
		},
		{
			name:    "File to extract not found",
			args:    args{zipPath: zipFile, fileName: "999.txt"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "File to extract not found",
			args:    args{zipPath: zipFile, fileName: "."},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "File to extract not found",
			args:    args{zipPath: zipFile, fileName: ".."},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "File to extract not found",
			args:    args{zipPath: zipFile, fileName: zipFile},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnzipFile(tt.args.zipPath, tt.args.fileName)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnzipFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UnzipFile() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnzip(t *testing.T) {
	zipPath := "./testdata/files.zip"

	destDir, err := os.MkdirTemp("", "unzipped_files")
	if err != nil {
		t.Fatalf("Не удалось создать временную папку: %v", err)
	}
	defer os.RemoveAll(destDir)

	err = Unzip(zipPath, destDir)
	if err != nil {
		t.Fatalf("Ошибка при распаковке: %v", err)
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("Ошибка при открытии ZIP-архива: %v", err)
	}
	defer r.Close()

	for _, f := range r.File {
		extractedPath := filepath.Join(destDir, f.Name)
		if _, err := os.Stat(extractedPath); os.IsNotExist(err) {
			t.Errorf("Файл %s не был извлечен", f.Name)
		}
	}
}
