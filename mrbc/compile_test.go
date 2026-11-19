package mrbc_test

import (
	"bytes"
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
		{"alternation pattern", "case 1\nin 1 | 2\n  puts 1\nend\n", "alternation patterns"},
		{"find pattern", "case [1, 2]\nin [*, 1, *]\n  puts 1\nend\n", "find patterns"},
		{"rest pattern", "case [1]\nin [a, *b]\n  puts a\nend\n", "array pattern rest"},
		{"it parameter", "[1].map { it * 2 }\n", "the 'it' block parameter"},
		{"dynamic constant path operator", "self.class::X += 1\n", "dynamic constant paths"},
		{"BEGIN block", "BEGIN { puts 1 }\n", "PreExecutionNode"},
		{"END block", "END { puts 1 }\n", "PostExecutionNode"},
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

func TestDebugInfoLineNumbers(t *testing.T) {
	mruby, err := toolchain.Find("mruby")
	if err != nil {
		t.Skip("mruby not found in PATH")
	}
	if err := toolchain.RequireVersion(mruby, []string{"-v"}, "mruby 4.0"); err != nil {
		t.Skipf("mruby 4.0.0 is required: %v", err)
	}

	dir := t.TempDir()
	source := filepath.Join(dir, "test.rb")
	if err := os.WriteFile(source, []byte("def f\n  x = 1\n  raise \"boom\"\nend\nf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binary, err := mrbc.CompileFile(context.Background(), source, mrbc.Options{DebugInfo: true})
	if err != nil {
		t.Fatalf("CompileFile: %v", err)
	}
	if !bytes.Contains(binary, []byte("DBG\x00")) {
		t.Fatal("debug binary does not contain a DBG section")
	}
	if !bytes.Contains(binary, []byte("LVAR")) {
		t.Fatal("debug binary does not contain an LVAR section")
	}
	out := filepath.Join(dir, "test.mrb")
	if err := os.WriteFile(out, binary, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := toolchain.Run(mruby, out)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("exit status = %d, want 1\nstdout=%s\nstderr=%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "test.rb:3:") {
		t.Fatalf("stderr has no source line: %q", result.Stderr)
	}

	noLV, err := mrbc.CompileFile(context.Background(), source, mrbc.Options{RemoveLV: true})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(noLV, []byte("LVAR")) {
		t.Error("RemoveLV binary still contains LVAR")
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
