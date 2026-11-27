package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run("gomrbc", args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeSource(t *testing.T, dir, name, src string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVersionAndCopyright(t *testing.T) {
	t.Run("version", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "--version")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stdout, "gomrbc 0.1.0") || !strings.Contains(stdout, "mruby 4.0.0") {
			t.Fatalf("version output = %q", stdout)
		}
		if stderr != "" {
			t.Fatalf("stderr = %q, want empty", stderr)
		}
	})
	t.Run("copyright", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "--copyright")
		if code != 0 {
			t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
		}
		if !strings.Contains(stdout, "Copyright") {
			t.Fatalf("copyright output = %q", stdout)
		}
		if stderr != "" {
			t.Fatalf("stderr = %q, want empty", stderr)
		}
	})
}

func TestUsageAndArgumentErrors(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "-h")
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stdout, "Usage: gomrbc [switches] programfile...") {
			t.Fatalf("stdout = %q, want usage", stdout)
		}
		if stderr != "" {
			t.Fatalf("stderr = %q, want empty", stderr)
		}
	})
	t.Run("unknown option", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "--bogus")
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stdout, "Usage: gomrbc") {
			t.Fatalf("stdout = %q, want usage", stdout)
		}
		if stderr != "" {
			t.Fatalf("stderr = %q, want empty", stderr)
		}
	})
	t.Run("no program file", func(t *testing.T) {
		code, stdout, stderr := runCLI(t)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if stdout != "" {
			t.Fatalf("stdout = %q, want empty", stdout)
		}
		if !strings.Contains(stderr, "gomrbc: no program file given") {
			t.Fatalf("stderr = %q", stderr)
		}
	})
	t.Run("duplicate output", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "-o", "a.mrb", "-o", "b.mrb", "x.rb")
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "an output file is already specified. (a.mrb)") {
			t.Fatalf("stderr = %q", stderr)
		}
		if !strings.Contains(stdout, "Usage: gomrbc") {
			t.Fatalf("stdout = %q, want usage", stdout)
		}
	})
	t.Run("multiple source files", func(t *testing.T) {
		dir := t.TempDir()
		one := writeSource(t, dir, "a.rb", "puts 1\n")
		two := writeSource(t, dir, "b.rb", "puts 2\n")
		code, _, stderr := runCLI(t, "-o", filepath.Join(dir, "out.mrb"), one, two)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "multiple source files are not supported yet") {
			t.Fatalf("stderr = %q", stderr)
		}
	})
}

func TestOutputForms(t *testing.T) {
	dir := t.TempDir()
	src := writeSource(t, dir, "hello.rb", "puts 1\n")

	t.Run("attached -o", func(t *testing.T) {
		out := filepath.Join(dir, "attached.mrb")
		code, stdout, stderr := runCLI(t, "-o"+out, src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		if stdout != "" || stderr != "" {
			t.Fatalf("stdout = %q, stderr = %q", stdout, stderr)
		}
		assertRITE(t, out)
	})
	t.Run("equals -o", func(t *testing.T) {
		out := filepath.Join(dir, "equals.mrb")
		code, _, stderr := runCLI(t, "-o="+out, src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		assertRITE(t, out)
	})
	t.Run("stdout", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "-o", "-", src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		if !strings.HasPrefix(stdout, "RITE") {
			t.Fatalf("stdout does not start with RITE: %q", stdout[:min(8, len(stdout))])
		}
		if stderr != "" {
			t.Fatalf("stderr = %q", stderr)
		}
	})
	t.Run("default name", func(t *testing.T) {
		code, _, stderr := runCLI(t, src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		assertRITE(t, filepath.Join(dir, "hello.mrb"))
	})
	t.Run("cannot open output", func(t *testing.T) {
		code, _, stderr := runCLI(t, "-o", filepath.Join(dir, "missing", "out.mrb"), src)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr, "cannot open output file:") {
			t.Fatalf("stderr = %q", stderr)
		}
	})
}

func TestSyntaxCheckAndVerbose(t *testing.T) {
	dir := t.TempDir()
	src := writeSource(t, dir, "check.rb", "puts 1\n")

	t.Run("syntax check", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "-c", src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		want := fmt.Sprintf("gomrbc:%s:Syntax OK\n", src)
		if stdout != want {
			t.Fatalf("stdout = %q, want %q", stdout, want)
		}
		if _, err := os.Stat(filepath.Join(dir, "check.mrb")); !os.IsNotExist(err) {
			t.Fatalf("syntax check created an output file")
		}
	})
	t.Run("-v prints version", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "-v", "-c", src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		if !strings.HasPrefix(stdout, "gomrbc 0.1.0") {
			t.Fatalf("stdout = %q, want version first", stdout)
		}
		if !strings.Contains(stdout, "irep[0]") {
			t.Fatalf("stdout = %q, want disassembly", stdout)
		}
	})
	t.Run("--verbose omits version", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "--verbose", "-c", src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		if !strings.HasPrefix(stdout, "irep[0]") {
			t.Fatalf("stdout = %q, want disassembly without version", stdout)
		}
	})
}

func TestDeprecatedEEOption(t *testing.T) {
	dir := t.TempDir()
	src := writeSource(t, dir, "e.rb", "puts 1\n")
	for _, flag := range []string{"-e", "-E"} {
		t.Run(flag, func(t *testing.T) {
			out := filepath.Join(dir, strings.TrimPrefix(flag, "-")+".mrb")
			code, stdout, stderr := runCLI(t, flag, "-o", out, src)
			if code != 0 {
				t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "-e/-E option no longer needed.") {
				t.Fatalf("stderr = %q", stderr)
			}
			assertRITE(t, out)
		})
	}
}

func TestNoExtOps(t *testing.T) {
	dir := t.TempDir()
	var src strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&src, "puts %q\n", fmt.Sprintf("s%d", i))
	}
	path := writeSource(t, dir, "manypool.rb", src.String())

	code, _, stderr := runCLI(t, "--no-ext-ops", "-o", filepath.Join(dir, "noext.mrb"), path)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "need OP_EXTs instruction (currently OP_EXTs are prohibited)") {
		t.Fatalf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, path+":257:") {
		t.Fatalf("stderr = %q, want the offending line", stderr)
	}

	code, _, stderr = runCLI(t, "-o", filepath.Join(dir, "ext.mrb"), path)
	if code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
	}
}

func TestDebugSectionsAndExtensions(t *testing.T) {
	dir := t.TempDir()
	src := writeSource(t, dir, "sections.rb", "puts 1\n")

	t.Run("debug info", func(t *testing.T) {
		out := filepath.Join(dir, "debug.mrb")
		code, _, stderr := runCLI(t, "-g", "-o", out, src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte("DBG\x00")) {
			t.Fatal("output has no DBG section")
		}
	})
	t.Run("remove local variables", func(t *testing.T) {
		localSrc := writeSource(t, dir, "locals.rb", "x = 1\nputs x\n")
		withLV := filepath.Join(dir, "with-lv.mrb")
		withoutLV := filepath.Join(dir, "without-lv.mrb")
		if code, _, stderr := runCLI(t, "-o", withLV, localSrc); code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		if code, _, stderr := runCLI(t, "--remove-lv", "-o", withoutLV, localSrc); code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		with, err := os.ReadFile(withLV)
		if err != nil {
			t.Fatal(err)
		}
		without, err := os.ReadFile(withoutLV)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(with, []byte("LVAR")) {
			t.Fatal("default output has no LVAR section")
		}
		if bytes.Contains(without, []byte("LVAR")) {
			t.Fatal("--remove-lv output still has an LVAR section")
		}
	})
	t.Run("dump ast", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "--dump-ast", src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		if strings.TrimSpace(stdout) == "" {
			t.Fatal("AST dump is empty")
		}
	})
	t.Run("dump ir", func(t *testing.T) {
		code, stdout, stderr := runCLI(t, "--dump-ir", src)
		if code != 0 {
			t.Fatalf("exit code = %d (stderr: %s)", code, stderr)
		}
		if strings.TrimSpace(stdout) == "" {
			t.Fatal("IR dump is empty")
		}
	})
}

func TestUnsupportedCOutput(t *testing.T) {
	dir := t.TempDir()
	src := writeSource(t, dir, "c.rb", "puts 1\n")
	code, _, stderr := runCLI(t, "-Bfoo", "-o", filepath.Join(dir, "out.c"), src)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "option -B (C output) is not supported yet") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func assertRITE(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("RITE")) {
		t.Fatalf("%s does not start with RITE", path)
	}
}
