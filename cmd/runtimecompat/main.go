// Command runtimecompat compares the runtime behavior of go-mrbc and mrbc.
//
// For each Ruby test file it builds a standalone program by prepending mruby's
// assert.rb and appending a deterministic summary line, compiles the program
// with both compilers, runs both binaries with mruby, and compares stdout,
// stderr, and exit status.
//
// mruby's own test files are written for mrbtest, which supplies t_print and
// calls report. This command supplies both so the files can run under a plain
// mruby binary.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/onohiroki/go-mrbc/mrbc"
)

type fileResult struct {
	Path     string `json:"path"`
	Status   string `json:"status"` // match, output, status, ref-compile, our-compile, both-compile, timeout
	TimedOut bool   `json:"timed_out,omitempty"`
	RefError string `json:"ref_error,omitempty"`
	OurError string `json:"our_error,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type report struct {
	Files        int            `json:"files"`
	Match        int            `json:"match"`
	Output       int            `json:"output"`
	Status       int            `json:"status"`
	RefCompile   int            `json:"ref_compile"`
	OurCompile   int            `json:"our_compile"`
	BothCompile  int            `json:"both_compile"`
	Timeout      int            `json:"timeout"`
	TimeoutMatch int            `json:"timeout_match"`
	Reasons      map[string]int `json:"reasons"`
	Results      []fileResult   `json:"results"`
	WrapperBytes int            `json:"wrapper_bytes"`
}

func main() {
	var (
		mrbcPath = flag.String("mrbc", "mrbc", "path to mrbc")
		mruby    = flag.String("mruby", "mruby", "path to mruby")
		assert   = flag.String("assert", "mruby/test/assert.rb", "path to mruby's test/assert.rb")
		jsonOut  = flag.String("json", "", "write a JSON report to this path")
		verbose  = flag.Bool("v", false, "print every file result")
		failDiff = flag.Bool("fail", false, "exit non-zero on compile or runtime mismatches")
		keep     = flag.Bool("keep", false, "keep generated .rb/.mrb files")
		workdir  = flag.String("workdir", "", "directory for generated files")
		timeout  = flag.Duration("timeout", 30*time.Second, "per-run timeout")
		retries  = flag.Int("retries", 2, "retries for runs that die from a signal")
	)
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"mruby/test/t", "mruby/mrbgems"}
	}

	ref, err := exec.LookPath(*mrbcPath)
	if err != nil {
		fail("mrbc not found: %v", err)
	}
	vm, err := exec.LookPath(*mruby)
	if err != nil {
		fail("mruby not found: %v", err)
	}
	if err := checkVersion(ref); err != nil {
		fail("%v", err)
	}
	assertSource, err := os.ReadFile(*assert)
	if err != nil {
		fail("read assert.rb: %v", err)
	}

	work := *workdir
	if work == "" {
		work, err = os.MkdirTemp("", "go-mrbc-runtime-")
		if err != nil {
			fail("%v", err)
		}
	} else if err := os.MkdirAll(work, 0o755); err != nil {
		fail("%v", err)
	}
	if !*keep && *workdir == "" {
		defer os.RemoveAll(work)
	}

	files, err := collectTests(roots)
	if err != nil {
		fail("%v", err)
	}

	prelude := "def t_print(*args); print(*args); end\n"
	epilogue := "\nputs \"__GOMRBC_SUMMARY ok=#{$ok_test} ko=#{$ko_test} crash=#{$kill_test} warn=#{$warning_test} skip=#{$skip_test} stress=#{$stress_test}\"\n"

	rep := report{Reasons: make(map[string]int)}
	ctx := context.Background()
	for i, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			fail("read %s: %v", path, err)
		}
		wrapper := filepath.Join(work, fmt.Sprintf("%03d.rb", i))
		combined := []byte(prelude + string(assertSource) + "\n" + string(source) + epilogue)
		rep.WrapperBytes += len(combined)
		if err := os.WriteFile(wrapper, combined, 0o644); err != nil {
			fail("write wrapper: %v", err)
		}

		result := fileResult{Path: path}
		refBin := filepath.Join(work, fmt.Sprintf("%03d-ref.mrb", i))
		ourBin := filepath.Join(work, fmt.Sprintf("%03d-our.mrb", i))
		refErr := compileReference(ref, wrapper, refBin)
		ourBinary, ourCompileErr := mrbc.CompileFile(ctx, wrapper, mrbc.Options{})
		if ourCompileErr != nil {
			result.OurError = ourCompileErr.Error()
		}

		switch {
		case refErr != "" && ourCompileErr != nil:
			result.Status = "both-compile"
			result.RefError = refErr
			rep.BothCompile++
			rep.Reasons["both compilers reject"]++
		case refErr != "":
			result.Status = "ref-compile"
			result.RefError = refErr
			rep.RefCompile++
			rep.Reasons[normalizeReason(refErr)]++
		case ourCompileErr != nil:
			result.Status = "our-compile"
			rep.OurCompile++
			rep.Reasons[normalizeReason(result.OurError)]++
		default:
			if err := os.WriteFile(ourBin, ourBinary, 0o644); err != nil {
				fail("write our binary: %v", err)
			}
			refRun, refTimedOut := run(vm, refBin, *timeout)
			ourRun, ourTimedOut := run(vm, ourBin, *timeout)
			switch {
			case refTimedOut && ourTimedOut:
				result.TimedOut = true
				if refRun.stdout == ourRun.stdout && refRun.stderr == ourRun.stderr {
					result.Status = "match"
					rep.Match++
					rep.TimeoutMatch++
				} else {
					result.Status = "output"
					result.Detail = "timeout; " + firstDifference(refRun, ourRun)
					rep.Output++
					rep.Reasons["runtime output mismatch (both timed out)"]++
				}
			case refTimedOut:
				result.Status = "timeout"
				rep.Timeout++
				rep.Reasons["reference timed out"]++
			case ourTimedOut:
				result.Status = "timeout"
				rep.Timeout++
				rep.Reasons["go-mrbc timed out"]++
			default:
				status, detail := classifyRun(refRun, ourRun)
				for attempt := 0; attempt < *retries && status == "status" && (refRun.exit < 0 || ourRun.exit < 0); attempt++ {
					refRun, _ = run(vm, refBin, *timeout)
					ourRun, _ = run(vm, ourBin, *timeout)
					status, detail = classifyRun(refRun, ourRun)
				}
				switch status {
				case "match":
					result.Status = "match"
					rep.Match++
				case "status":
					result.Status = "status"
					result.Detail = detail
					rep.Status++
					rep.Reasons["exit status mismatch"]++
				default:
					result.Status = "output"
					result.Detail = detail
					rep.Output++
					rep.Reasons["runtime output mismatch"]++
				}
			}
		}

		rep.Results = append(rep.Results, result)
		if *verbose || (result.Status != "match" && result.Status != "both-compile") {
			fmt.Printf("%-12s %s\n", result.Status, path)
			if result.Detail != "" {
				fmt.Printf("  %s\n", result.Detail)
			}
			if result.RefError != "" {
				fmt.Printf("  mrbc:    %s\n", result.RefError)
			}
			if result.OurError != "" {
				fmt.Printf("  go-mrbc: %s\n", result.OurError)
			}
		}
	}
	rep.Files = len(files)

	printSummary(&rep)
	if *jsonOut != "" {
		data, err := json.MarshalIndent(&rep, "", "  ")
		if err != nil {
			fail("%v", err)
		}
		if err := os.WriteFile(*jsonOut, data, 0o644); err != nil {
			fail("%v", err)
		}
		fmt.Printf("report written to %s\n", *jsonOut)
	}
	if *failDiff && (rep.Output > 0 || rep.Status > 0 || rep.RefCompile > 0 || rep.OurCompile > 0 || rep.Timeout > 0) {
		os.Exit(1)
	}
}

type runResult struct {
	stdout string
	stderr string
	exit   int
}

func run(mruby, binary string, timeout time.Duration) (runResult, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, mruby, binary)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := runResult{stdout: stdout.String(), stderr: stderr.String()}
	if ctx.Err() == context.DeadlineExceeded {
		return result, true
	}
	if err == nil {
		return result, false
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.exit = exitErr.ExitCode()
		return result, false
	}
	result.exit = -1
	result.stderr = err.Error()
	return result, false
}

func classifyRun(ref, our runResult) (string, string) {
	if ref.exit == our.exit && ref.stdout == our.stdout && ref.stderr == our.stderr {
		return "match", ""
	}
	if ref.exit != our.exit {
		return "status", fmt.Sprintf("exit: mrbc=%d go-mrbc=%d", ref.exit, our.exit)
	}
	return "output", firstDifference(ref, our)
}

func firstDifference(ref, our runResult) string {
	if ref.exit != our.exit {
		return fmt.Sprintf("exit: mrbc=%d go-mrbc=%d", ref.exit, our.exit)
	}
	if ref.stdout != our.stdout {
		return "stdout: " + diffPosition(ref.stdout, our.stdout)
	}
	if ref.stderr != our.stderr {
		return "stderr: " + diffPosition(ref.stderr, our.stderr)
	}
	return ""
}

func diffPosition(ref, our string) string {
	refLines := strings.Split(ref, "\n")
	ourLines := strings.Split(our, "\n")
	for i := 0; i < len(refLines) && i < len(ourLines); i++ {
		if refLines[i] != ourLines[i] {
			return fmt.Sprintf("line %d: mrbc %q, go-mrbc %q", i+1, refLines[i], ourLines[i])
		}
	}
	return fmt.Sprintf("length: mrbc %d, go-mrbc %d", len(ref), len(our))
}

func checkVersion(mrbcPath string) error {
	out, err := exec.Command(mrbcPath, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("mrbc --version failed: %w", err)
	}
	if !strings.Contains(string(out), "4.0") {
		return fmt.Errorf("mrbc 4.0.0 is required, got: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func collectTests(roots []string) ([]string, error) {
	var files []string
	seen := make(map[string]bool)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if filepath.Ext(path) != ".rb" || seen[path] {
				return nil
			}
			// Only test files; mrblib and tools are not standalone tests.
			slash := filepath.ToSlash(path)
			base := filepath.Base(path)
			if base == "assert.rb" || base == "bintest.rb" {
				return nil
			}
			if strings.Contains(slash, "/bintest/") ||
				strings.Contains(slash, "/tools/") ||
				strings.Contains(slash, "/bench/") ||
				strings.Contains(slash, "/benchmark/") ||
				strings.Contains(slash, "/mrblib/") {
				return nil
			}
			if !strings.Contains(slash, "/test/") && !strings.Contains(slash, "/test/t/") {
				return nil
			}
			seen[path] = true
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func compileReference(mrbcPath, source, output string) string {
	cmd := exec.Command(mrbcPath, "-o", output, source)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stderr.String())
	}
	return ""
}

func normalizeReason(message string) string {
	if index := strings.LastIndex(message, ": "); index >= 0 {
		return message[index+2:]
	}
	return message
}

func printSummary(rep *report) {
	fmt.Printf("files:        %d\n", rep.Files)
	fmt.Printf("match:        %d\n", rep.Match)
	fmt.Printf("output:       %d\n", rep.Output)
	fmt.Printf("status:       %d\n", rep.Status)
	fmt.Printf("ref compile:  %d\n", rep.RefCompile)
	fmt.Printf("our compile:  %d\n", rep.OurCompile)
	fmt.Printf("both compile: %d\n", rep.BothCompile)
	fmt.Printf("timeout:      %d\n", rep.Timeout)
	if rep.TimeoutMatch > 0 {
		fmt.Printf("timeout match:%d\n", rep.TimeoutMatch)
	}
	if len(rep.Reasons) == 0 {
		return
	}
	fmt.Println("\nreasons:")
	type entry struct {
		reason string
		count  int
	}
	var entries []entry
	for reason, count := range rep.Reasons {
		entries = append(entries, entry{reason, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].reason < entries[j].reason
	})
	for _, item := range entries {
		fmt.Printf("  %4d  %s\n", item.count, item.reason)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "runtimecompat: "+format+"\n", args...)
	os.Exit(1)
}
