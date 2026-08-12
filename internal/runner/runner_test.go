package runner

import "testing"

func TestShellQuote(t *testing.T) {
	if got := ShellQuote("a'b"); got != `'"'"'a'"'"'b'"'"'` && got != `'a'"'"'b'` {
		// 标准形式：'a'"'"'b'
		if got != `'a'"'"'b'` {
			t.Fatalf("got %q", got)
		}
	}
}

func TestWrapWorkdir(t *testing.T) {
	got := WrapWorkdir("/var/log", "tail -n 20 app.log")
	want := "cd -- '/var/log' && tail -n 20 app.log"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if WrapWorkdir("", "true") != "true" {
		t.Fatal("empty workdir")
	}
}

func TestLimitedBufferTruncates(t *testing.T) {
	var b limitedBuffer
	b.limit = 5
	_, _ = b.Write([]byte("hello world"))
	if b.String() != "hello" {
		t.Fatalf("buf=%q", b.String())
	}
	if !b.truncated {
		t.Fatal("expected truncated")
	}
}
