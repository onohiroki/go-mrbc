package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/onohiroki/go-mrbc/bytecode"
	"github.com/onohiroki/go-mrbc/internal/compiler"
	"github.com/onohiroki/go-mrbc/internal/frontend"
	"github.com/onohiroki/go-mrbc/mrbc"
)

func main() {
	os.Exit(run(os.Args[0], os.Args[1:], os.Stdout, os.Stderr))
}

func run(prog string, args []string, stdout, stderr io.Writer) int {
	opts, files, action, err := parseArgs(args)
	if opts.printVersion {
		printVersion(stdout)
	}
	for _, warning := range opts.warnings {
		fmt.Fprintf(stderr, "%s: %s\n", prog, warning)
	}
	if err != nil {
		if !errors.Is(err, errHelp) {
			fmt.Fprintf(stderr, "%s: %v\n", prog, err)
		}
		usage(stdout, prog)
		return 1
	}

	switch action {
	case actionVersion:
		printVersion(stdout)
		return 0
	case actionCopyright:
		printCopyright(stdout)
		return 0
	}

	if len(files) == 0 {
		fmt.Fprintf(stderr, "%s: no program file given\n", prog)
		return 1
	}
	if len(files) > 1 {
		fmt.Fprintf(stderr, "%s: multiple source files are not supported yet\n", prog)
		return 1
	}

	path := files[0]
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", prog, err)
		return 1
	}
	ctx := context.Background()

	if opts.dumpAST {
		data, err := frontend.DumpAST(ctx, src, path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	}

	program, err := frontend.Lower(ctx, src, path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if opts.dumpIR {
		if err := frontend.Dump(stdout, program); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}

	irep, err := compiler.CompileWithOptions(program, path, src, compiler.Options{
		NoExtOps: opts.noExtOps,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if opts.verbose {
		if err := bytecode.Disassemble(stdout, irep); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	if opts.checkOnly {
		fmt.Fprintf(stdout, "%s:%s:Syntax OK\n", prog, path)
		return 0
	}

	binary, err := bytecode.DumpBinaryWithOptions(irep, bytecode.DumpOptions{
		DebugInfo: opts.debugInfo,
		RemoveLV:  opts.removeLV,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	output := opts.output
	if !opts.outputSet {
		base := strings.TrimSuffix(path, filepath.Ext(path))
		output = base + ".mrb"
	}
	if output == "-" {
		if _, err := stdout.Write(binary); err != nil {
			fmt.Fprintf(stderr, "%s: cannot write output: %v\n", prog, err)
			return 1
		}
		return 0
	}
	if err := os.WriteFile(output, binary, 0o644); err != nil {
		fmt.Fprintf(stderr, "%s: cannot open output file:(%s)\n", prog, output)
		return 1
	}
	return 0
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "gomrbc %s (%s)\n", mrbc.Version, mrbc.Target)
}

func printCopyright(w io.Writer) {
	fmt.Fprintln(w, "gomrbc - Copyright (c) 2026 onohiroki and go-mrbc contributors")
}
