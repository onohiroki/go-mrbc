// Command compat compares go-mrbc against mrbc on a directory of Ruby files.
//
// For each file it runs mrbc and the go-mrbc compiler.
// It reports files that mrbc accepts but go-mrbc rejects, grouped by reason.
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

	"github.com/onohiroki/go-mrbc/mrbc"
)

type fileResult struct {
	Path     string `json:"path"`
	Status   string `json:"status"` // ok, gap, superset, both-fail
	RefError string `json:"ref_error,omitempty"`
	OurError string `json:"our_error,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type report struct {
	Files    int            `json:"files"`
	OK       int            `json:"ok"`
	Gaps     int            `json:"gaps"`
	Superset int            `json:"superset"`
	BothFail int            `json:"both_fail"`
	Reasons  map[string]int `json:"reasons"`
	Results  []fileResult   `json:"results"`
}

func main() {
	var (
		mrbcPath = flag.String("mrbc", "mrbc", "path to mrbc")
		jsonOut  = flag.String("json", "", "write a JSON report to this path")
		verbose  = flag.Bool("v", false, "print every file result")
		failGaps = flag.Bool("fail", false, "exit non-zero when a compile gap is found")
		keepMrb  = flag.Bool("keep", false, "keep reference .mrb files (see -workdir)")
		workdir  = flag.String("workdir", "", "directory for reference .mrb files")
	)
	flag.Parse()
	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"mruby/test", "mruby/mrbgems"}
	}

	ref, err := exec.LookPath(*mrbcPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compat: mrbc not found: %v\n", err)
		os.Exit(1)
	}
	if err := checkMrbcVersion(ref); err != nil {
		fmt.Fprintf(os.Stderr, "compat: %v\n", err)
		os.Exit(1)
	}

	work := *workdir
	cleanup := func() {}
	if work == "" {
		work, err = os.MkdirTemp("", "go-mrbc-compat-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "compat: %v\n", err)
			os.Exit(1)
		}
		if !*keepMrb {
			cleanup = func() { os.RemoveAll(work) }
		}
	}
	defer cleanup()

	files, err := collectRubyFiles(roots)
	if err != nil {
		fmt.Fprintf(os.Stderr, "compat: %v\n", err)
		os.Exit(1)
	}

	rep := report{Reasons: make(map[string]int)}
	ctx := context.Background()
	for i, path := range files {
		refErr := compileReference(ref, path, filepath.Join(work, fmt.Sprintf("%d.mrb", i)))
		_, ourErr := mrbc.CompileFile(ctx, path, mrbc.Options{RemoveLV: true})
		result := fileResult{Path: path}
		switch {
		case refErr == "" && ourErr == nil:
			result.Status = "ok"
			rep.OK++
		case refErr == "" && ourErr != nil:
			result.Status = "gap"
			result.OurError = ourErr.Error()
			result.Reason = normalizeReason(result.OurError)
			rep.Gaps++
			rep.Reasons[result.Reason]++
		case refErr != "" && ourErr == nil:
			result.Status = "superset"
			result.RefError = refErr
			rep.Superset++
		default:
			result.Status = "both-fail"
			result.RefError = refErr
			result.OurError = ourErr.Error()
			rep.BothFail++
		}
		rep.Results = append(rep.Results, result)
		if *verbose {
			fmt.Printf("%-10s %s\n", result.Status, path)
			if result.OurError != "" {
				fmt.Printf("  go-mrbc: %s\n", result.OurError)
			}
			if result.RefError != "" {
				fmt.Printf("  mrbc:    %s\n", result.RefError)
			}
		}
	}
	rep.Files = len(files)

	printSummary(&rep)
	if *jsonOut != "" {
		data, err := json.MarshalIndent(&rep, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "compat: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*jsonOut, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "compat: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("report written to %s\n", *jsonOut)
	}
	if *failGaps && rep.Gaps > 0 {
		os.Exit(1)
	}
}

func checkMrbcVersion(path string) error {
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("mrbc --version failed: %w", err)
	}
	if !strings.Contains(string(out), "4.0") {
		return fmt.Errorf("mrbc 4.0.0 is required, got: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func collectRubyFiles(roots []string) ([]string, error) {
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
	fmt.Printf("files:      %d\n", rep.Files)
	fmt.Printf("ok:         %d\n", rep.OK)
	fmt.Printf("gaps:       %d\n", rep.Gaps)
	fmt.Printf("superset:   %d\n", rep.Superset)
	fmt.Printf("both fail:  %d\n", rep.BothFail)
	if len(rep.Reasons) == 0 {
		return
	}
	fmt.Println("\ngap reasons:")
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
