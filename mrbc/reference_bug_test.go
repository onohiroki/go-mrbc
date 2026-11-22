package mrbc_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/onohiroki/go-mrbc/mrbc"
	"github.com/onohiroki/go-mrbc/toolchain"
)

// These cases pass with go-mrbc but fail with the reference mrbc 4.0.0
// release binary because of compiler bugs that were fixed later in the mruby
// development tree. They stay out of testdata/conformance, which compares
// against the reference compiler, and assert the CRuby behavior directly.

// The reference compiler folds `q + -5` into SUBI 5, which calls `-` on an
// operator-redefined receiver instead of `+` with -5. go-mrbc only folds
// non-negative immediates, matching Ruby and mruby's later fix (#2557,
// mruby commits b4f645050 and 9f66fb063).
func TestReferenceNegativeImmediateOperatorOverrideBug(t *testing.T) {
	src := `class Q
  def +(x); ["add", x]; end
  def -(x); ["sub", x]; end
end
q = Q.new
p q + -5
p q - -5
p q + 5
p q - 5
p q + -500
p q - -500
`
	want := "[\"add\", -5]\n[\"sub\", -5]\n[\"add\", 5]\n[\"sub\", 5]\n[\"add\", -500]\n[\"sub\", -500]\n"
	if got := compileAndRunOnMruby(t, src, "negative_immediate.rb"); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

// The reference compiler lets `next` in a rescue clause escape the assert
// block instead of continuing the enclosing loop, so the assertion never
// runs. mruby fixed this in commit c4627a60f.
func TestReferenceNextInsideRescueBug(t *testing.T) {
	src := `i = 0
v = while true
  i += 1
  begin
    raise "e" if i < 3
    break i * 100
  rescue
    next
  end
end
puts v
puts i
`
	want := "300\n3\n"
	if got := compileAndRunOnMruby(t, src, "next_in_rescue.rb"); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

// compileAndRunOnMruby compiles src with go-mrbc and runs the bytecode with
// the reference mruby binary. It skips when mruby is unavailable.
func compileAndRunOnMruby(t *testing.T, src, file string) string {
	t.Helper()
	mruby, err := toolchain.Find("mruby")
	if err != nil {
		t.Skip("mruby not found in PATH")
	}
	if err := toolchain.RequireVersion(mruby, []string{"-v"}, "mruby 4.0"); err != nil {
		t.Skipf("mruby 4.0.0 is required: %v", err)
	}

	irep, err := mrbc.Compile(context.Background(), []byte(src), mrbc.Options{FilePath: file})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	binary, err := mrbc.Dump(irep)
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.mrb")
	if err := os.WriteFile(path, binary, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := toolchain.Run(mruby, path)
	if err != nil {
		t.Fatalf("run mruby: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("mruby: exit=%d stdout=%q stderr=%q", result.ExitCode, result.Stdout, result.Stderr)
	}
	return result.Stdout
}
