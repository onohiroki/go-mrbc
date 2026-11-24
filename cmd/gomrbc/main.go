package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onohiroki/go-mrbc/bytecode"
	"github.com/onohiroki/go-mrbc/internal/compiler"
	"github.com/onohiroki/go-mrbc/internal/frontend"
	"github.com/onohiroki/go-mrbc/mrbc"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("gomrbc", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)

	var (
		output     string
		checkOnly  bool
		verbose    bool
		dumpAST    bool
		dumpIR     bool
		showVer    bool
		removeLV   bool
		noOptimize bool
		debugInfo  bool
	)
	flags.StringVar(&output, "o", "", "write bytecode to `file`")
	flags.BoolVar(&checkOnly, "c", false, "check syntax only")
	flags.BoolVar(&verbose, "v", false, "print the generated instruction sequence")
	flags.BoolVar(&verbose, "verbose", false, "print the generated instruction sequence")
	flags.BoolVar(&dumpAST, "dump-ast", false, "print the Prism AST as JSON")
	flags.BoolVar(&dumpIR, "dump-ir", false, "print the lowered IR")
	flags.BoolVar(&showVer, "version", false, "print version information")
	flags.BoolVar(&removeLV, "remove-lv", false, "omit local variable information")
	flags.BoolVar(&noOptimize, "no-optimize", false, "disable optimization (currently always on)")
	flags.BoolVar(&debugInfo, "g", false, "emit debug information")
	flags.BoolVar(&debugInfo, "debug", false, "emit debug information")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: gomrbc [options] file.rb\n\nOptions:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if showVer {
		fmt.Printf("gomrbc %s (%s)\n", mrbc.Version, mrbc.Target)
		return nil
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return fmt.Errorf("gomrbc: expected exactly one source file")
	}
	path := flags.Arg(0)
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	ctx := context.Background()

	if dumpAST {
		data, err := frontend.DumpAST(ctx, src, path)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	program, err := frontend.Lower(ctx, src, path)
	if err != nil {
		return err
	}
	if dumpIR {
		if err := frontend.Dump(os.Stdout, program); err != nil {
			return err
		}
		return nil
	}

	irep, err := compiler.Compile(program, path, src)
	if err != nil {
		return err
	}
	if verbose {
		if err := bytecode.Disassemble(os.Stdout, irep); err != nil {
			return err
		}
	}
	if checkOnly {
		fmt.Printf("%s: Syntax OK\n", path)
		return nil
	}

	binary, err := bytecode.DumpBinaryWithOptions(irep, bytecode.DumpOptions{
		DebugInfo: debugInfo,
		RemoveLV:  removeLV,
	})
	if err != nil {
		return err
	}
	if output == "" {
		base := strings.TrimSuffix(path, filepath.Ext(path))
		output = base + ".mrb"
	}
	if err := os.WriteFile(output, binary, 0o644); err != nil {
		return err
	}
	_ = noOptimize
	return nil
}
