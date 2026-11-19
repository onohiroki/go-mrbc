package mrbc_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/onohiroki/go-mrbc/mrbc"
	"github.com/onohiroki/go-mrbc/toolchain"
)

// TestRandomizedDifferential generates small programs within the supported
// subset, compiles them with mrbc and go-mrbc, runs both binaries on mruby,
// and compares stdout, stderr, and exit status.
func TestRandomizedDifferential(t *testing.T) {
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

	iterations := 50
	if testing.Short() {
		iterations = 10
	}
	if value := os.Getenv("GOMRBC_FUZZ_ITERATIONS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			t.Fatalf("invalid GOMRBC_FUZZ_ITERATIONS=%q", value)
		}
		iterations = parsed
	}
	seed := int64(1)
	if value := os.Getenv("GOMRBC_FUZZ_SEED"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			t.Fatalf("invalid GOMRBC_FUZZ_SEED=%q", value)
		}
		seed = parsed
	}

	rng := rand.New(rand.NewSource(seed))
	ctx := context.Background()
	for i := 0; i < iterations; i++ {
		sourceText := generateProgram(rng)
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "main.rb")
			reference := filepath.Join(dir, "reference.mrb")
			oursBinary := filepath.Join(dir, "ours.mrb")
			if err := os.WriteFile(source, []byte(sourceText), 0o644); err != nil {
				t.Fatal(err)
			}

			ref, err := toolchain.Run(mrbcPath, "-o", reference, source)
			if err != nil {
				t.Fatalf("run mrbc: %v", err)
			}
			if ref.ExitCode != 0 {
				t.Fatalf("mrbc rejected the generated program:\n%s\n%s", sourceText, ref.Stderr)
			}

			ours, err := mrbc.CompileFile(ctx, source, mrbc.Options{})
			if err != nil {
				t.Fatalf("go-mrbc rejected the generated program:\n%s\n%v", sourceText, err)
			}
			if err := os.WriteFile(oursBinary, ours, 0o644); err != nil {
				t.Fatal(err)
			}

			refRun, err := toolchain.Run(mruby, reference)
			if err != nil {
				t.Fatalf("run reference: %v", err)
			}
			ourRun, err := toolchain.Run(mruby, oursBinary)
			if err != nil {
				t.Fatalf("run go-mrbc binary: %v", err)
			}

			if refRun.ExitCode != ourRun.ExitCode ||
				refRun.Stdout != ourRun.Stdout ||
				refRun.Stderr != ourRun.Stderr {
				t.Fatalf("runtime mismatch (seed=%d)\n"+
					"program:\n%s\n"+
					"mrbc: exit=%d stdout=%q stderr=%q\n"+
					"ours: exit=%d stdout=%q stderr=%q",
					seed, sourceText,
					refRun.ExitCode, refRun.Stdout, refRun.Stderr,
					ourRun.ExitCode, ourRun.Stdout, ourRun.Stderr)
			}
		})
	}
}

func generateProgram(rng *rand.Rand) string {
	var b strings.Builder
	b.WriteString("def m(a, b = 2, *rest, k: 1, **opts)\n")
	b.WriteString("  [a, b, rest, k, opts]\n")
	b.WriteString("end\n")

	x := generateExpression(rng, 2)
	y := generateExpression(rng, 2)
	fmt.Fprintf(&b, "x = %s\n", x)
	fmt.Fprintf(&b, "y = %s\n", y)
	b.WriteString("p x\np y\np(x + y)\np(x * y - 3)\n")
	b.WriteString("if x > y\n  p :gt\nelse\n  p :le\nend\n")
	b.WriteString("p [x, y, 3].map { |v| v + 1 }\n")
	b.WriteString("p m(x, k: y)\n")
	b.WriteString("p m(x, y, 4, k: 5, z: 6) { }\n")
	b.WriteString("case [x, y]\nin [a, b]\n  p a + b\nelse\n  p :none\nend\n")
	b.WriteString("p \"v=#{x}:#{y}\"\n")
	b.WriteString("h = { a: x, b: y }\np h[:a] + h[:b]\n")
	b.WriteString("s = 0\ni = 0\nwhile i < 3\n  s += x\n  i += 1\nend\np s\n")
	b.WriteString("p (1..3).map { |i| i * x }\n")
	return b.String()
}

func generateExpression(rng *rand.Rand, depth int) string {
	if depth <= 0 || rng.Intn(3) == 0 {
		return strconv.Itoa(rng.Intn(21) - 10)
	}
	switch rng.Intn(4) {
	case 0:
		return fmt.Sprintf("(%s + %s)", generateExpression(rng, depth-1), generateExpression(rng, depth-1))
	case 1:
		return fmt.Sprintf("(%s - %s)", generateExpression(rng, depth-1), generateExpression(rng, depth-1))
	case 2:
		return fmt.Sprintf("(%s * %s)", generateExpression(rng, depth-1), generateExpression(rng, depth-1))
	default:
		return fmt.Sprintf("-(%s)", generateExpression(rng, depth-1))
	}
}
