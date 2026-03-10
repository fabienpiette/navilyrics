package tagger

import (
	"testing"
)

func TestParseLRC(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []syncedLine
	}{
		{
			name:  "basic two lines",
			input: "[00:01.00] Line one\n[00:02.50] Line two",
			want: []syncedLine{
				{ms: 1000, text: "Line one"},
				{ms: 2500, text: "Line two"},
			},
		},
		{
			name:  "three-digit centiseconds",
			input: "[01:30.500] Hello",
			want:  []syncedLine{{ms: 90500, text: "Hello"}},
		},
		{
			name:  "metadata lines skipped",
			input: "[ti:My Song]\n[ar:Artist]\n[00:01.00] Lyric",
			want:  []syncedLine{{ms: 1000, text: "Lyric"}},
		},
		{
			name:  "empty lines skipped",
			input: "\n[00:01.00] Text\n\n",
			want:  []syncedLine{{ms: 1000, text: "Text"}},
		},
		{
			name:  "multiple timestamps on one line",
			input: "[00:01.00][00:45.00] Chorus",
			want: []syncedLine{
				{ms: 1000, text: "Chorus"},
				{ms: 45000, text: "Chorus"},
			},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLRC(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("want %d lines, got %d: %v", len(tc.want), len(got), got)
			}
			for i, w := range tc.want {
				if got[i].ms != w.ms {
					t.Errorf("line %d: want ms=%d, got ms=%d", i, w.ms, got[i].ms)
				}
				if got[i].text != w.text {
					t.Errorf("line %d: want text=%q, got text=%q", i, w.text, got[i].text)
				}
			}
		})
	}
}
