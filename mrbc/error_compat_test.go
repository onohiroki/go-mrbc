package mrbc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/onohiroki/go-mrbc/toolchain"
)

// TestErrorCompatibility checks that invalid programs fail in both compilers
// and that the CLI reports a located error with the same exit status as mrbc.
//
// The exact wording of the diagnostics is not compared: Prism and mruby's
// parser produce different messages. Exit status, the presence of a
// file:line:column location, and the fact that both reject the program are
// compared.
func TestErrorCompatibility(t *testing.T) {
	mrbcPath, err := toolchain.Find("mrbc")
	if err != nil {
		t.Skip("mrbc not found in PATH")
	}
	if err := toolchain.RequireVersion(mrbcPath, []string{"--version"}, "4.0"); err != nil {
		t.Skipf("mrbc 4.0.0 is required: %v", err)
	}

	gomrbc := buildGomrbc(t)

	cases := []struct {
		name string
		src  string
	}{
		{"unclosed def", "def f(\n"},
		{"unclosed if", "if true\n  puts 1\n"},
		{"missing rhs", "x =\n"},
		{"dangling operator", "1 +\n"},
		{"class without name", "class\n"},
		{"BEGIN block", "BEGIN { puts 1 }\n"},
		{"global variable alias", "alias $a $b\n"},
		{"subjectless case/in", "case\nin 1\nend\n"},
	}

	location := regexp.MustCompile(`:\d+:\d+:`)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "case.rb")
			if err := os.WriteFile(source, []byte(tt.src), 0o644); err != nil {
				t.Fatal(err)
			}

			ref, err := toolchain.Run(mrbcPath, "-c", source)
			if err != nil {
				t.Fatalf("run mrbc: %v", err)
			}
			if ref.ExitCode == 0 {
				t.Fatalf("mrbc accepted the invalid program:\n%s", tt.src)
			}
			if strings.TrimSpace(ref.Stderr) == "" {
				t.Fatal("mrbc produced no diagnostic")
			}

			ours, err := toolchain.Run(gomrbc, "-c", source)
			if err != nil {
				t.Fatalf("run gomrbc: %v", err)
			}
			if ours.ExitCode == 0 {
				t.Fatalf("gomrbc accepted the invalid program:\n%s", tt.src)
			}
			if ours.ExitCode != ref.ExitCode {
				t.Fatalf("exit status: gomrbc=%d, mrbc=%d", ours.ExitCode, ref.ExitCode)
			}
			if strings.TrimSpace(ours.Stderr) == "" {
				t.Fatal("gomrbc produced no diagnostic")
			}
			if !strings.Contains(ours.Stderr, "case.rb:") {
				t.Fatalf("gomrbc diagnostic does not name the source file: %s", ours.Stderr)
			}
			if !location.MatchString(ours.Stderr) {
				t.Fatalf("gomrbc diagnostic has no line:column location: %s", ours.Stderr)
			}
		})
	}
}

func buildGomrbc(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "gomrbc")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/gomrbc")
	cmd.Dir = filepath.Dir(dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build gomrbc: %v\n%s", err, output)
	}
	return binary
}
