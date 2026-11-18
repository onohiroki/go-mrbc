package mrbc_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onohiroki/go-mrbc/internal/frontend"
	"github.com/onohiroki/go-mrbc/mrbc"
	"github.com/onohiroki/go-mrbc/toolchain"
)

func TestUnsupportedSyntax(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"defined", "defined?(x)\n", "defined?"},
		{"rescue", "begin\n  foo\nrescue\n  bar\nend\n", "begin/rescue/ensure"},
		{"for destructuring", "for a, b in [[1, 2]]\n  puts a\nend\n", "unsupported multiple assignment target"},
		{"index target", "a = [1]\na[0], b = [1, 2]\n", "unsupported multiple assignment target"},
		{"keyword and splat", "a = [1]\nf(*a, k: 1)\n", "keyword arguments combined with splat"},
		{"keyword splat and keys", "h = {}\nf(**h, k: 1)\n", "keyword splats combined with explicit keywords"},
		{"post parameter", "def f(a, *b, c)\nend\n", "post parameters"},
		{"anonymous block parameter", "def f(&)\nend\n", "anonymous block parameters"},
		{"safe navigation", "x = nil\nx&.to_s\n", "safe navigation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mrbc.Compile(context.Background(), []byte(tt.src), mrbc.Options{FilePath: "test.rb"})
			if err == nil {
				t.Fatal("expected compile error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "test.rb:") {
				t.Fatalf("error has no file location: %q", err)
			}
		})
	}
}

func TestCompileRunsOnMruby(t *testing.T) {
	mruby, err := toolchain.Find("mruby")
	if err != nil {
		t.Skip("mruby not found in PATH")
	}
	if err := toolchain.RequireVersion(mruby, []string{"-v"}, "mruby 4.0"); err != nil {
		t.Skipf("mruby 4.0.0 is required: %v", err)
	}

	src := []byte("puts \"compiled\"\n")
	binary, err := mrbc.CompileFile(context.Background(), writeTemp(t, src), mrbc.Options{})
	if err != nil {
		t.Fatalf("CompileFile: %v", err)
	}
	path := filepath.Join(t.TempDir(), "out.mrb")
	if err := os.WriteFile(path, binary, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := toolchain.Run(mruby, path)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Stdout != "compiled\n" {
		t.Fatalf("mruby: exit=%d stdout=%q stderr=%q", result.ExitCode, result.Stdout, result.Stderr)
	}
}

func TestLowerNestedBlockDepths(t *testing.T) {
	src := []byte("x = 1\n[1].each do |i|\n  [2].each do |j|\n    x = x + i + j\n  end\nend\n")
	program, err := frontend.Lower(context.Background(), src, "depth.rb")
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	var b strings.Builder
	if err := frontend.Dump(&b, program); err != nil {
		t.Fatalf("Dump: %v", err)
	}
	dump := b.String()
	for _, want := range []string{
		"lvar-write x depth=2",
		"lvar x depth=2",
		"lvar i depth=1",
		"lvar j depth=0",
	} {
		if !strings.Contains(dump, want) {
			t.Fatalf("IR dump does not contain %q:\n%s", want, dump)
		}
	}
}

func TestPrismSyntaxErrorHasPosition(t *testing.T) {
	src := []byte("def broken(\n")
	_, err := frontend.Lower(context.Background(), src, "broken.rb")
	if err == nil {
		t.Fatal("expected syntax error")
	}
	if !strings.Contains(err.Error(), "broken.rb:1:") {
		t.Fatalf("error has no position: %q", err)
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.rb")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
