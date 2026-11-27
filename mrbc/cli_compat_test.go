package mrbc_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onohiroki/go-mrbc/toolchain"
)

// TestCLIOptionCompatibility checks the switches that go-mrbc intentionally
// shares with mrbc. The exact prose is not compared; exit status and the
// observable shape of stdout/stderr are.
func TestCLIOptionCompatibility(t *testing.T) {
	mrbcPath, err := toolchain.Find("mrbc")
	if err != nil {
		t.Skip("mrbc not found in PATH")
	}
	if err := toolchain.RequireVersion(mrbcPath, []string{"--version"}, "4.0"); err != nil {
		t.Skipf("mrbc 4.0.0 is required: %v", err)
	}
	gomrbc := buildGomrbc(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.rb")
	if err := os.WriteFile(src, []byte("x = 1\nputs x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	type result struct {
		stdout string
		stderr string
		exit   int
	}
	runBoth := func(t *testing.T, args ...string) (result, result) {
		t.Helper()
		ref, err := toolchain.Run(mrbcPath, args...)
		if err != nil {
			t.Fatal(err)
		}
		ours, err := toolchain.Run(gomrbc, args...)
		if err != nil {
			t.Fatal(err)
		}
		return result{ref.Stdout, ref.Stderr, ref.ExitCode}, result{ours.Stdout, ours.Stderr, ours.ExitCode}
	}

	t.Run("version", func(t *testing.T) {
		ref, ours := runBoth(t, "--version")
		if ref.exit != 0 || ours.exit != 0 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		if !strings.Contains(ours.stdout, "4.0") {
			t.Fatalf("gomrbc version = %q", ours.stdout)
		}
	})

	t.Run("copyright", func(t *testing.T) {
		ref, ours := runBoth(t, "--copyright")
		if ref.exit != 0 || ours.exit != 0 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		if strings.TrimSpace(ref.stdout) == "" || strings.TrimSpace(ours.stdout) == "" {
			t.Fatal("copyright output is empty")
		}
	})

	t.Run("help", func(t *testing.T) {
		ref, ours := runBoth(t, "-h")
		if ref.exit != 1 || ours.exit != 1 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		if !strings.Contains(ref.stdout, "Usage:") || !strings.Contains(ours.stdout, "Usage:") {
			t.Fatalf("stdout: mrbc=%q gomrbc=%q", ref.stdout, ours.stdout)
		}
		if ref.stderr != "" || ours.stderr != "" {
			t.Fatalf("stderr: mrbc=%q gomrbc=%q", ref.stderr, ours.stderr)
		}
	})

	t.Run("unknown option", func(t *testing.T) {
		ref, ours := runBoth(t, "--bogus")
		if ref.exit != 1 || ours.exit != 1 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		if !strings.Contains(ref.stdout, "Usage:") || !strings.Contains(ours.stdout, "Usage:") {
			t.Fatalf("stdout: mrbc=%q gomrbc=%q", ref.stdout, ours.stdout)
		}
	})

	t.Run("no program file", func(t *testing.T) {
		ref, ours := runBoth(t)
		if ref.exit != 1 || ours.exit != 1 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		if !strings.Contains(ref.stderr, "no program file given") ||
			!strings.Contains(ours.stderr, "no program file given") {
			t.Fatalf("stderr: mrbc=%q gomrbc=%q", ref.stderr, ours.stderr)
		}
		if ref.stdout != "" || ours.stdout != "" {
			t.Fatalf("stdout: mrbc=%q gomrbc=%q", ref.stdout, ours.stdout)
		}
	})

	t.Run("syntax check", func(t *testing.T) {
		ref, ours := runBoth(t, "-c", src)
		if ref.exit != 0 || ours.exit != 0 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		suffix := ":" + src + ":Syntax OK\n"
		if !strings.HasSuffix(ref.stdout, suffix) || !strings.HasSuffix(ours.stdout, suffix) {
			t.Fatalf("stdout: mrbc=%q gomrbc=%q", ref.stdout, ours.stdout)
		}
	})

	t.Run("attached output", func(t *testing.T) {
		refOut := filepath.Join(dir, "attached-ref.mrb")
		ourOut := filepath.Join(dir, "attached-our.mrb")
		ref, err := toolchain.Run(mrbcPath, "-o"+refOut, src)
		if err != nil {
			t.Fatal(err)
		}
		ours, err := toolchain.Run(gomrbc, "-o"+ourOut, src)
		if err != nil {
			t.Fatal(err)
		}
		if ref.ExitCode != 0 || ours.ExitCode != 0 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.ExitCode, ours.ExitCode)
		}
		assertRITEFile(t, refOut)
		assertRITEFile(t, ourOut)
	})

	t.Run("stdout output", func(t *testing.T) {
		ref, ours := runBoth(t, "-o", "-", src)
		if ref.exit != 0 || ours.exit != 0 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		if !strings.HasPrefix(ref.stdout, "RITE") || !strings.HasPrefix(ours.stdout, "RITE") {
			t.Fatalf("stdout does not start with RITE")
		}
	})

	t.Run("deprecated -e", func(t *testing.T) {
		refOut := filepath.Join(dir, "e-ref.mrb")
		ourOut := filepath.Join(dir, "e-our.mrb")
		ref, err := toolchain.Run(mrbcPath, "-e", "-o", refOut, src)
		if err != nil {
			t.Fatal(err)
		}
		ours, err := toolchain.Run(gomrbc, "-e", "-o", ourOut, src)
		if err != nil {
			t.Fatal(err)
		}
		if ref.ExitCode != 0 || ours.ExitCode != 0 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.ExitCode, ours.ExitCode)
		}
		if !strings.Contains(ref.Stderr, "-e/-E option no longer needed.") ||
			!strings.Contains(ours.Stderr, "-e/-E option no longer needed.") {
			t.Fatalf("stderr: mrbc=%q gomrbc=%q", ref.Stderr, ours.Stderr)
		}
	})

	t.Run("duplicate output", func(t *testing.T) {
		ref, ours := runBoth(t, "-o", filepath.Join(dir, "a.mrb"), "-o", filepath.Join(dir, "b.mrb"), src)
		if ref.exit != 1 || ours.exit != 1 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		if !strings.Contains(ref.stderr, "an output file is already specified") ||
			!strings.Contains(ours.stderr, "an output file is already specified") {
			t.Fatalf("stderr: mrbc=%q gomrbc=%q", ref.stderr, ours.stderr)
		}
	})

	t.Run("verbose version", func(t *testing.T) {
		ref, ours := runBoth(t, "-v", "-c", src)
		if ref.exit != 0 || ours.exit != 0 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		if !strings.Contains(ref.stdout, "Syntax OK") || !strings.Contains(ours.stdout, "Syntax OK") {
			t.Fatalf("stdout has no Syntax OK")
		}
		if strings.HasPrefix(ref.stdout, "irep[") || strings.HasPrefix(ours.stdout, "irep[") {
			t.Fatal("-v did not print a version line first")
		}
	})

	t.Run("debug info", func(t *testing.T) {
		refOut := filepath.Join(dir, "debug-ref.mrb")
		ourOut := filepath.Join(dir, "debug-our.mrb")
		ref, err := toolchain.Run(mrbcPath, "-g", "-o", refOut, src)
		if err != nil {
			t.Fatal(err)
		}
		ours, err := toolchain.Run(gomrbc, "-g", "-o", ourOut, src)
		if err != nil {
			t.Fatal(err)
		}
		if ref.ExitCode != 0 || ours.ExitCode != 0 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.ExitCode, ours.ExitCode)
		}
		assertSection(t, refOut, "DBG\x00")
		assertSection(t, ourOut, "DBG\x00")
	})

	t.Run("remove-lv", func(t *testing.T) {
		refOut := filepath.Join(dir, "nolv-ref.mrb")
		ourOut := filepath.Join(dir, "nolv-our.mrb")
		ref, err := toolchain.Run(mrbcPath, "--remove-lv", "-o", refOut, src)
		if err != nil {
			t.Fatal(err)
		}
		ours, err := toolchain.Run(gomrbc, "--remove-lv", "-o", ourOut, src)
		if err != nil {
			t.Fatal(err)
		}
		if ref.ExitCode != 0 || ours.ExitCode != 0 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.ExitCode, ours.ExitCode)
		}
		assertNoSection(t, refOut, "LVAR")
		assertNoSection(t, ourOut, "LVAR")
	})

	t.Run("no-ext-ops", func(t *testing.T) {
		var source strings.Builder
		for i := 0; i < 300; i++ {
			fmt.Fprintf(&source, "puts %q\n", fmt.Sprintf("s%d", i))
		}
		many := filepath.Join(dir, "manypool.rb")
		if err := os.WriteFile(many, []byte(source.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		ref, ours := runBoth(t, "--no-ext-ops", "-o", filepath.Join(dir, "noext.mrb"), many)
		if ref.exit != 1 || ours.exit != 1 {
			t.Fatalf("exit: mrbc=%d gomrbc=%d", ref.exit, ours.exit)
		}
		want := many + ":257:"
		if !strings.Contains(ref.stderr, want) || !strings.Contains(ours.stderr, want) {
			t.Fatalf("stderr: mrbc=%q gomrbc=%q", ref.stderr, ours.stderr)
		}
		if !strings.Contains(ref.stderr, "need OP_EXTs instruction") ||
			!strings.Contains(ours.stderr, "need OP_EXTs instruction") {
			t.Fatalf("stderr: mrbc=%q gomrbc=%q", ref.stderr, ours.stderr)
		}
	})
}

func assertRITEFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("RITE")) {
		t.Fatalf("%s does not start with RITE", path)
	}
}

func assertSection(t *testing.T, path, section string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(section)) {
		t.Fatalf("%s has no %q section", path, section)
	}
}

func assertNoSection(t *testing.T, path, section string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(section)) {
		t.Fatalf("%s still has a %q section", path, section)
	}
}
