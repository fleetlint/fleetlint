package builtin

import (
	"io"
	"strings"
	"testing"
)

func TestTailBufferKeepsOnlyTheTail(t *testing.T) {
	t.Parallel()
	tb := &tailBuffer{limit: 10}
	// io.Copy is how os/exec feeds the buffer; it must not get around Write.
	n, err := io.Copy(tb, strings.NewReader(strings.Repeat("a", 25)+"0123456789"))
	if n != 35 || err != nil || tb.String() != "0123456789" {
		t.Fatalf("n=%d err=%v tail=%q", n, err, tb.String())
	}
	if _, err := tb.Write([]byte("xyz")); err != nil || tb.String() != "3456789xyz" {
		t.Fatalf("after a further write: %q %v", tb.String(), err)
	}
}

func TestLastLines(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"", ""},
		{"only\n", "only"},
		{"a\nb\nc\nd\ne\nf\n\n", "b | c | d | e | f"},
		{"a\nb", "a | b"},
	}
	for _, c := range cases {
		if got := lastLines(c.in, 5); got != c.want {
			t.Errorf("lastLines(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCheckCommandPerRunner(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"make": "make check", "just": "just check", "task": "task check",
		"npm-scripts": "npm run check", "gradle": "./gradlew check --quiet", "none": "", "": "",
	}
	for kind, want := range cases {
		cmd, ok := checkCommand(kind)
		if got := strings.Join(cmd, " "); got != want || ok != (want != "") {
			t.Errorf("checkCommand(%q) = %q, %v; want %q", kind, got, ok, want)
		}
	}
}
