package printer

import (
	"reflect"
	"testing"
)

func TestSplitMetaCommands(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []segment
	}{
		{
			name:  "no backslash",
			input: "SELECT 1;\n",
			want:  []segment{{text: "SELECT 1;\n"}},
		},
		{
			name:  "meta between statements",
			input: "SELECT 1;\n\\restrict abc\nSELECT 2;\n",
			want: []segment{
				{text: "SELECT 1;\n"},
				{meta: true, text: "\\restrict abc\n"},
				{text: "SELECT 2;\n"},
			},
		},
		{
			name:  "meta with leading whitespace and CRLF",
			input: "\t\\connect postgres\r\n",
			want: []segment{
				{text: "\t"},
				{meta: true, text: "\\connect postgres\r\n"},
			},
		},
		{
			name:  "meta without trailing newline",
			input: "\\unrestrict abc",
			want:  []segment{{meta: true, text: "\\unrestrict abc"}},
		},
		{
			name:  "backslash line inside E-string with escaped quote",
			input: "SELECT E'quote: \\' still open\n\\restrict nope\n';\n",
			want:  []segment{{text: "SELECT E'quote: \\' still open\n\\restrict nope\n';\n"}},
		},
		{
			name:  "dollar parameter does not open a dollar quote",
			input: "PREPARE p AS SELECT $1;\n\\restrict abc\n",
			want: []segment{
				{text: "PREPARE p AS SELECT $1;\n"},
				{meta: true, text: "\\restrict abc\n"},
			},
		},
		{
			name:  "backslash mid-line is not a meta-command",
			input: "SELECT 'a' \\ 'b';\n",
			want:  []segment{{text: "SELECT 'a' \\ 'b';\n"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitMetaCommands(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitMetaCommands(%q)\n got: %#v\nwant: %#v", tt.input, got, tt.want)
			}
		})
	}
}
