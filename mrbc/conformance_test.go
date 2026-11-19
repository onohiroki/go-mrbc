package mrbc_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/onohiroki/go-mrbc/mrbc"
	"github.com/onohiroki/go-mrbc/toolchain"
)

func TestConformance(t *testing.T) {
	mruby, err := toolchain.Find("mruby")
	if err != nil {
		t.Skip("mruby not found in PATH")
	}
	mrbcPath, err := toolchain.Find("mrbc")
	if err != nil {
		t.Skip("mrbc not found in PATH")
	}
	if err := toolchain.RequireVersion(mruby, []string{"-v"}, "mruby 4.0"); err != nil {
		t.Skipf("mruby 4.0.0 is required: %v", err)
	}

	root := filepath.Join("..", "testdata", "conformance")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read conformance directory: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(root, name)
			mainPath := filepath.Join(dir, "main.rb")
			if _, err := os.Stat(mainPath); err != nil {
				t.Fatalf("missing main.rb: %v", err)
			}

			tmp := t.TempDir()
			wantStdout, err := os.ReadFile(filepath.Join(dir, "stdout.txt"))
			if err != nil {
				t.Fatalf("read stdout.txt: %v", err)
			}
			wantStatus := 0
			if data, err := os.ReadFile(filepath.Join(dir, "status.txt")); err == nil {
				status, err := strconv.Atoi(strings.TrimSpace(string(data)))
				if err != nil {
					t.Fatalf("parse status.txt: %v", err)
				}
				wantStatus = status
			}
			args := readArgs(t, filepath.Join(dir, "args.txt"))

			reference := filepath.Join(tmp, "reference.mrb")
			result, err := toolchain.Run(mrbcPath, "-o", reference, mainPath)
			if err != nil {
				t.Fatalf("run mrbc: %v", err)
			}
			if result.ExitCode != 0 {
				t.Fatalf("mrbc failed: %s", result.Stderr)
			}

			oursBinary := filepath.Join(tmp, "ours.mrb")
			ours, err := mrbc.CompileFile(context.Background(), mainPath, mrbc.Options{})
			if err != nil {
				t.Fatalf("compile with go-mrbc: %v", err)
			}
			if err := os.WriteFile(oursBinary, ours, 0o644); err != nil {
				t.Fatal(err)
			}

			refRun := runMruby(t, mruby, reference, args)
			ourRun := runMruby(t, mruby, oursBinary, args)

			if refRun.ExitCode != wantStatus {
				t.Fatalf("reference exit status = %d, want %d\n%s", refRun.ExitCode, wantStatus, refRun.Stderr)
			}
			if ourRun.ExitCode != wantStatus {
				t.Fatalf("go-mrbc exit status = %d, want %d\n%s", ourRun.ExitCode, wantStatus, ourRun.Stderr)
			}
			if refRun.Stdout != string(wantStdout) {
				t.Fatalf("reference stdout mismatch:\n got %q\nwant %q", refRun.Stdout, string(wantStdout))
			}
			if ourRun.Stdout != string(wantStdout) {
				t.Fatalf("go-mrbc stdout mismatch:\n got %q\nwant %q", ourRun.Stdout, string(wantStdout))
			}
			if ourRun.Stdout != refRun.Stdout {
				t.Fatalf("go-mrbc and mrbc disagree:\n go-mrbc %q\n mrbc    %q", ourRun.Stdout, refRun.Stdout)
			}
			if wantStderr, err := os.ReadFile(filepath.Join(dir, "stderr.txt")); err == nil {
				if ourRun.Stderr != string(wantStderr) {
					t.Fatalf("go-mrbc stderr mismatch:\n got %q\nwant %q", ourRun.Stderr, string(wantStderr))
				}
			}
		})
	}
}

func runMruby(t *testing.T, mruby, path string, args []string) toolchain.Result {
	t.Helper()
	result, err := toolchain.Run(mruby, append([]string{path}, args...)...)
	if err != nil {
		t.Fatalf("run mruby: %v", err)
	}
	return result
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read args.txt: %v", err)
	}
	var args []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			args = append(args, line)
		}
	}
	return args
}
