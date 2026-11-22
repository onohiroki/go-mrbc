package mrbc_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/onohiroki/go-mrbc/conformance"
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

	cases, err := conformance.Cases()
	if err != nil {
		t.Fatalf("list conformance cases: %v", err)
	}

	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			source, err := fs.ReadFile(conformance.FS, name+"/main.rb")
			if err != nil {
				t.Fatalf("read main.rb: %v", err)
			}
			wantStdout, err := fs.ReadFile(conformance.FS, name+"/stdout.txt")
			if err != nil {
				t.Fatalf("read stdout.txt: %v", err)
			}
			wantStatus := 0
			if data, ok := readOptional(t, name+"/status.txt"); ok {
				status, err := strconv.Atoi(strings.TrimSpace(string(data)))
				if err != nil {
					t.Fatalf("parse status.txt: %v", err)
				}
				wantStatus = status
			}
			args := readArgs(t, name+"/args.txt")
			wantStderr, hasStderr := readOptional(t, name+"/stderr.txt")

			tmp := t.TempDir()
			mainPath := filepath.Join(tmp, "main.rb")
			if err := os.WriteFile(mainPath, source, 0o644); err != nil {
				t.Fatal(err)
			}
			ours, err := mrbc.CompileFile(context.Background(), mainPath, mrbc.Options{FilePath: name + "/main.rb"})
			if err != nil {
				t.Fatalf("compile with go-mrbc: %v", err)
			}

			reference := filepath.Join(tmp, "reference.mrb")
			result, err := toolchain.Run(mrbcPath, "-o", reference, mainPath)
			if err != nil {
				t.Fatalf("run mrbc: %v", err)
			}
			if result.ExitCode != 0 {
				t.Fatalf("mrbc failed: %s", result.Stderr)
			}

			oursBinary := filepath.Join(tmp, "ours.mrb")
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
			if hasStderr && ourRun.Stderr != string(wantStderr) {
				t.Fatalf("go-mrbc stderr mismatch:\n got %q\nwant %q", ourRun.Stderr, string(wantStderr))
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

func readOptional(t *testing.T, name string) ([]byte, bool) {
	t.Helper()
	data, err := fs.ReadFile(conformance.FS, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false
		}
		t.Fatalf("read %s: %v", name, err)
	}
	return data, true
}

func readArgs(t *testing.T, name string) []string {
	t.Helper()
	data, ok := readOptional(t, name)
	if !ok {
		return nil
	}
	var args []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			args = append(args, line)
		}
	}
	return args
}
