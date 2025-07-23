package storage

import "testing"

func Test_replaceInvalidFilenameChars(t *testing.T) {
	type args struct {
		filename string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Valid filename",
			args: args{filename: "my_document.txt"},
			want: "my_document.txt",
		},
		{
			name: "Windows invalid chars",
			args: args{filename: "report<>.pdf"},
			want: "report__.pdf",
		},
		{
			name: "Windows invalid chars",
			args: args{filename: "report:.pdf"},
			want: "report_.pdf",
		},
		{
			name: "Windows invalid chars",
			args: args{filename: "report\\.pdf"},
			want: "report_.pdf",
		},
		{
			name: "Windows invalid chars",
			args: args{filename: "report*.pdf"},
			want: "report_.pdf",
		},
		{
			name: "Windows invalid chars",
			args: args{filename: "report?.pdf"},
			want: "report_.pdf",
		},
		{
			name: "Unix path separator",
			args: args{filename: "path/to/file.doc"},
			want: "path_to_file.doc",
		},
		{
			name: "Control characters",
			args: args{filename: "file\x00with\tcontrol\x7Fchars.log"},
			want: "file_with_control_chars.log",
		},
		{
			name: "Mixed invalid chars",
			args: args{filename: "my<bad>file:name?/with\\all*the|things.txt"},
			want: "my_bad_file_name__with_all_the_things.txt",
		},
		{
			name: "Filename ending with space (Windows)",
			args: args{filename: "document .doc"},
			want: "document .doc", // Note: The TrimRight only affects trailing spaces, not internal ones.
		},
		{
			name: "Empty string",
			args: args{filename: ""},
			want: "",
		},
		{
			name: "Only invalid characters",
			args: args{filename: "<>:/\\"},
			want: "_____",
		},
		{
			name: "Filename with allowed Unicode characters",
			args: args{filename: "файл_с_русскими_буквами.txt"},
			want: "файл_с_русскими_буквами.txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replaceInvalidFilenameChars(tt.args.filename); got != tt.want {
				t.Errorf("ReplaceInvalidFilenameChars() = %v, want %v", got, tt.want)
			}
		})
	}
}
