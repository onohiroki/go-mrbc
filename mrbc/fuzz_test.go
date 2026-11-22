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
	b.WriteString("def pick(a, b = 2, *rest, k: 1, **opts)\n")
	b.WriteString("  [a, b, rest, k, opts]\n")
	b.WriteString("end\n")
	b.WriteString("def fact(n)\n")
	b.WriteString("  if n < 1\n    1\n  else\n    n * fact(n - 1)\n  end\n")
	b.WriteString("end\n")
	b.WriteString("def arith(a, b)\n  (a + b) * (a - b)\nend\n")
	b.WriteString("def apply(x)\n  yield(x)\nend\n")

	var vars []string
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("v%d", i)
		fmt.Fprintf(&b, "%s = %s\n", name, generateIntExpr(rng, 2, vars))
		vars = append(vars, name)
	}

	fmt.Fprintf(&b, "f = %s\n", generateFloatExpr(rng, 1))
	fmt.Fprintf(&b, "s = %s\n", generateStringExpr(rng, 1, vars))
	fmt.Fprintf(&b, "a = %s\n", generateArrayExpr(rng, vars))
	fmt.Fprintf(&b, "h = { a: %s, b: %s, c: %s }\n",
		generateIntExpr(rng, 1, vars),
		generateIntExpr(rng, 1, vars),
		generateIntExpr(rng, 1, vars))

	for _, v := range vars {
		fmt.Fprintf(&b, "p %s\n", v)
	}
	b.WriteString("p f\np s\np a\np a.length\n")
	fmt.Fprintf(&b, "p a[%d]\n", rng.Intn(3))
	b.WriteString("p h[:a]\np h[:b]\np h.fetch(:a, 0)\n")

	fmt.Fprintf(&b, "p %s\n", generateIntExpr(rng, 2, vars))
	fmt.Fprintf(&b, "p %s\n", generateBoolExpr(rng, 2, vars))
	fmt.Fprintf(&b, "p %s\n", generateStringExpr(rng, 2, vars))
	fmt.Fprintf(&b, "p %s\n", generateFloatExpr(rng, 2))

	fmt.Fprintf(&b, "p arith(%s, %s)\n",
		generateIntExpr(rng, 1, vars),
		generateIntExpr(rng, 1, vars))
	fmt.Fprintf(&b, "p fact(%d)\n", rng.Intn(6))
	fmt.Fprintf(&b, "p pick(%s, k: %s)\n",
		generateIntExpr(rng, 1, vars),
		generateIntExpr(rng, 1, vars))
	fmt.Fprintf(&b, "p pick(%s, %s, 4, k: 5, z: 6) { }\n",
		generateIntExpr(rng, 1, vars),
		generateIntExpr(rng, 1, vars))
	fmt.Fprintf(&b, "p apply(%s) { |x| x * 2 + 1 }\n", generateIntExpr(rng, 1, vars))

	fmt.Fprintf(&b, "p [1, 2, 3].map { |v| v * %s }\n", vars[0])
	b.WriteString("total = 0\n")
	b.WriteString("[1, 2, 3, 4].each do |v|\n")
	fmt.Fprintf(&b, "  total += v * %s\n", vars[1])
	b.WriteString("end\np total\n")
	fmt.Fprintf(&b, "p (1..3).map { |i| i * %s }\n", vars[0])
	b.WriteString("p (1...4).to_a\n")
	writeConditional(&b, rng, vars)
	writeLoop(&b, rng, vars[2])
	writeForLoop(&b, vars[3])
	writeCase(&b, rng, vars[4])
	writeComparisons(&b, rng, vars)
	writePatterns(&b, vars)
	writeException(&b, rng, vars)
	writeMultipleAssignment(&b, rng, vars)
	writeClass(&b, rng, vars)
	return b.String()
}

func writeConditional(b *strings.Builder, rng *rand.Rand, vars []string) {
	fmt.Fprintf(b, "if %s\n  p(:if_true)\nelsif %s\n  p(:if_elsif)\nelse\n  p(:if_false)\nend\n",
		generateBoolExpr(rng, 1, vars),
		generateBoolExpr(rng, 1, vars))
}

func writeLoop(b *strings.Builder, rng *rand.Rand, v string) {
	limit := 3 + rng.Intn(3)
	fmt.Fprintf(b, "loop_sum = 0\nloop_i = 0\nwhile loop_i < %d\n", limit)
	b.WriteString("  loop_i += 1\n  next if loop_i == 2\n")
	fmt.Fprintf(b, "  loop_sum += loop_i * %s\n", v)
	b.WriteString("end\np loop_sum\n")
	fmt.Fprintf(b, "break_value = while true\n  loop_i += 1\n  break loop_i * %s if loop_i > %d\nend\np break_value\n", v, limit+2)
	b.WriteString("until_i = 0\nuntil until_i >= 3\n  until_i += 1\nend\np until_i\n")
}

func writeForLoop(b *strings.Builder, v string) {
	fmt.Fprintf(b, "for_sum = 0\nfor fv in [1, 2, 3]\n  for_sum += fv * %s\nend\np for_sum\n", v)
}

func writeCase(b *strings.Builder, rng *rand.Rand, v string) {
	fmt.Fprintf(b, "case %s\n", generateIntExpr(rng, 1, []string{v}))
	for i := 0; i < 2; i++ {
		fmt.Fprintf(b, "when %d\n  p(:case_%d)\n", rng.Intn(6), i)
	}
	b.WriteString("else\n  p(:case_else)\nend\n")
}

func writeComparisons(b *strings.Builder, rng *rand.Rand, vars []string) {
	cmp := []string{"<", "<=", ">", ">=", "==", "!="}
	for i := 0; i < 3; i++ {
		fmt.Fprintf(b, "p(%s %s %s)\n",
			generateIntExpr(rng, 1, vars),
			cmp[rng.Intn(len(cmp))],
			generateIntExpr(rng, 1, vars))
	}
	fmt.Fprintf(b, "p(%s)\n", generateBoolExpr(rng, 2, vars))
}

func writePatterns(b *strings.Builder, vars []string) {
	fmt.Fprintf(b, "case [%s, %s]\nin [pa, pb] if pa <= pb\n  p(pa + pb)\nin [pa, pb]\n  p(pa - pb)\nend\n",
		vars[0], vars[1])
	fmt.Fprintf(b, "case { a: %s, b: %s }\nin { a: ha, b: hb }\n  p(ha + hb)\nelse\n  p(:no_hash)\nend\n",
		vars[0], vars[1])
	b.WriteString("case [1, 2, 3]\nin [first, *rest]\n  p(first + rest.length)\nelse\n  p(:no_rest)\nend\n")
}

func writeException(b *strings.Builder, rng *rand.Rand, vars []string) {
	b.WriteString("exc = begin\n")
	fmt.Fprintf(b, "  raise \"boom\" if %s\n", generateBoolExpr(rng, 1, vars))
	b.WriteString("  :no_raise\nrescue => error\n  error.message\nensure\n  :ensured\nend\np exc\n")
	b.WriteString("begin\n  [1, 2, 3].fetch(10)\nrescue => e\n  p(e.class)\nend\n")
}

func writeMultipleAssignment(b *strings.Builder, rng *rand.Rand, vars []string) {
	fmt.Fprintf(b, "ma, mb = %s, %s\np(ma + mb)\n",
		generateIntExpr(rng, 1, vars),
		generateIntExpr(rng, 1, vars))
	b.WriteString("mc, *md = [1, 2, 3, 4]\np(mc)\np(md.length)\n")
	b.WriteString("(me, mf), mg = [1, 2], 3\np(me + mf + mg)\n")
}

func writeClass(b *strings.Builder, rng *rand.Rand, vars []string) {
	b.WriteString("class Box\n")
	b.WriteString("  attr_accessor :value\n")
	b.WriteString("  def initialize(value)\n    @value = value\n  end\n")
	b.WriteString("  def add(n)\n    @value + n\n  end\n")
	b.WriteString("  def self.twice(n)\n    n * 2\n  end\n")
	b.WriteString("end\n")
	fmt.Fprintf(b, "box = Box.new(%s)\np(box.value)\n", generateIntExpr(rng, 1, vars))
	fmt.Fprintf(b, "box.value = %s\np(box.value)\n", generateIntExpr(rng, 1, vars))
	fmt.Fprintf(b, "p(box.add(%s))\n", generateIntExpr(rng, 1, vars))
	fmt.Fprintf(b, "p(Box.twice(%s))\n", generateIntExpr(rng, 1, vars))
}

func generateIntExpr(rng *rand.Rand, depth int, vars []string) string {
	if depth <= 0 || rng.Intn(4) == 0 {
		return generateIntAtom(rng, vars)
	}
	left := generateIntExpr(rng, depth-1, vars)
	right := generateIntExpr(rng, depth-1, vars)
	switch rng.Intn(12) {
	case 0:
		return fmt.Sprintf("(%s + %s)", left, right)
	case 1:
		return fmt.Sprintf("(%s - %s)", left, right)
	case 2:
		return fmt.Sprintf("(%s * %s)", left, right)
	case 3:
		return fmt.Sprintf("(%s / %d)", left, 1+rng.Intn(9))
	case 4:
		return fmt.Sprintf("(%s %% %d)", left, 1+rng.Intn(9))
	case 5:
		return fmt.Sprintf("(%s & %s)", left, right)
	case 6:
		return fmt.Sprintf("(%s | %s)", left, right)
	case 7:
		return fmt.Sprintf("(%s ^ %s)", left, right)
	case 8:
		return fmt.Sprintf("(%s << %d)", left, rng.Intn(4))
	case 9:
		return fmt.Sprintf("(%s >> %d)", left, rng.Intn(4))
	case 10:
		return fmt.Sprintf("-(%s)", left)
	default:
		return fmt.Sprintf("(%s ? %s : %s)",
			generateBoolExpr(rng, depth-1, vars),
			generateIntExpr(rng, depth-1, vars),
			generateIntExpr(rng, depth-1, vars))
	}
}

func generateIntAtom(rng *rand.Rand, vars []string) string {
	if len(vars) > 0 && rng.Intn(4) == 0 {
		return vars[rng.Intn(len(vars))]
	}
	switch rng.Intn(12) {
	case 0:
		return "0"
	case 1:
		return "1"
	case 2:
		return "-1"
	case 3:
		return "255"
	case 4:
		return "256"
	case 5:
		return "-256"
	case 6:
		return "65535"
	case 7:
		return "-65536"
	default:
		return strconv.Itoa(rng.Intn(21) - 10)
	}
}

func generateBoolExpr(rng *rand.Rand, depth int, vars []string) string {
	if depth <= 0 || rng.Intn(4) == 0 {
		switch rng.Intn(5) {
		case 0:
			return "true"
		case 1:
			return "false"
		case 2:
			return "nil"
		case 3:
			return fmt.Sprintf("(%s).even?", generateIntExpr(rng, 0, vars))
		default:
			cmp := []string{"<", "<=", ">", ">=", "==", "!="}
			return fmt.Sprintf("(%s %s %s)",
				generateIntExpr(rng, 0, vars),
				cmp[rng.Intn(len(cmp))],
				generateIntExpr(rng, 0, vars))
		}
	}
	switch rng.Intn(4) {
	case 0:
		return fmt.Sprintf("(%s && %s)", generateBoolExpr(rng, depth-1, vars), generateBoolExpr(rng, depth-1, vars))
	case 1:
		return fmt.Sprintf("(%s || %s)", generateBoolExpr(rng, depth-1, vars), generateBoolExpr(rng, depth-1, vars))
	case 2:
		return fmt.Sprintf("!(%s)", generateBoolExpr(rng, depth-1, vars))
	default:
		cmp := []string{"<", "<=", ">", ">=", "==", "!="}
		return fmt.Sprintf("(%s %s %s)",
			generateIntExpr(rng, depth-1, vars),
			cmp[rng.Intn(len(cmp))],
			generateIntExpr(rng, depth-1, vars))
	}
}

func generateFloatExpr(rng *rand.Rand, depth int) string {
	if depth <= 0 || rng.Intn(3) == 0 {
		values := []string{"0.5", "1.5", "-2.25", "3.0", "10.25"}
		return values[rng.Intn(len(values))]
	}
	left := generateFloatExpr(rng, depth-1)
	right := generateFloatExpr(rng, depth-1)
	switch rng.Intn(4) {
	case 0:
		return fmt.Sprintf("(%s + %s)", left, right)
	case 1:
		return fmt.Sprintf("(%s - %s)", left, right)
	case 2:
		return fmt.Sprintf("(%s * %s)", left, right)
	default:
		return fmt.Sprintf("(%s / 1.5)", left)
	}
}

func generateStringExpr(rng *rand.Rand, depth int, vars []string) string {
	if depth <= 0 || rng.Intn(3) == 0 {
		values := []string{`"a"`, `"hello"`, `""`, `"x y"`}
		return values[rng.Intn(len(values))]
	}
	switch rng.Intn(4) {
	case 0:
		return fmt.Sprintf("(%s + %s)", generateStringExpr(rng, depth-1, vars), generateStringExpr(rng, depth-1, vars))
	case 1:
		return fmt.Sprintf("(%s * %d)", generateStringExpr(rng, depth-1, vars), 1+rng.Intn(3))
	case 2:
		return fmt.Sprintf("\"v=#{%s}\"", generateIntExpr(rng, depth-1, vars))
	default:
		return fmt.Sprintf("(%s + \"!\")", generateStringExpr(rng, depth-1, vars))
	}
}

func generateArrayExpr(rng *rand.Rand, vars []string) string {
	return fmt.Sprintf("[%s, %s, %s]",
		generateIntExpr(rng, 1, vars),
		generateIntExpr(rng, 1, vars),
		generateIntExpr(rng, 1, vars))
}
