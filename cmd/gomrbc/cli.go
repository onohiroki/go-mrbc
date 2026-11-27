package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

var errHelp = errors.New("help requested")

// cliOptions is the result of parsing the mrbc-compatible switches.
type cliOptions struct {
	output       string
	outputSet    bool
	checkOnly    bool
	verbose      bool
	printVersion bool
	debugInfo    bool
	removeLV     bool
	noExtOps     bool
	// noOptimize is accepted for mrbc compatibility. The compiler has no
	// peephole optimizer yet, so there is nothing to disable.
	noOptimize bool
	dumpAST    bool
	dumpIR     bool
	warnings   []string
}

type cliAction int

const (
	actionCompile cliAction = iota
	actionVersion
	actionCopyright
)

// parseArgs follows mrbc's command-line grammar: switches come first and the
// remaining arguments are source files. gomrbc-specific long options are
// accepted alongside the mrbc ones.
func parseArgs(args []string) (cliOptions, []string, cliAction, error) {
	var opts cliOptions
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			i++
			break
		}
		if arg == "" || arg == "-" || !strings.HasPrefix(arg, "-") {
			break
		}
		if strings.HasPrefix(arg, "--") {
			action, err := parseLongOption(&opts, arg[2:])
			if action != actionCompile || err != nil {
				return opts, nil, action, err
			}
			i++
			continue
		}
		// Keep accepting the single-dash long spellings that the previous
		// Go flag based CLI accepted, such as -remove-lv.
		if name := arg[1:]; isLongOption(name) {
			action, err := parseLongOption(&opts, name)
			if action != actionCompile || err != nil {
				return opts, nil, action, err
			}
			i++
			continue
		}
		switch arg[1] {
		case 'o':
			if opts.outputSet {
				return opts, nil, actionCompile, fmt.Errorf("an output file is already specified. (%s)", opts.output)
			}
			switch {
			case len(arg) > 2:
				opts.output = strings.TrimPrefix(arg[2:], "=")
			case i+1 < len(args):
				i++
				opts.output = args[i]
			default:
				return opts, nil, actionCompile, errors.New("option -o requires an argument")
			}
			opts.outputSet = true
		case 'B':
			return opts, nil, actionCompile, errors.New("option -B (C output) is not supported yet")
		case 'S', 's':
			// mrbc only uses these with -B, which is rejected above.
		case 'c':
			opts.checkOnly = true
		case 'v':
			opts.printVersion = true
			opts.verbose = true
		case 'g':
			opts.debugInfo = true
		case 'e', 'E':
			opts.warnings = append(opts.warnings, "-e/-E option no longer needed.")
		case 'h':
			return opts, nil, actionCompile, errHelp
		default:
			return opts, nil, actionCompile, errHelp
		}
		i++
	}
	return opts, args[i:], actionCompile, nil
}

func isLongOption(name string) bool {
	switch name {
	case "version", "verbose", "copyright", "remove-lv", "no-ext-ops",
		"no-optimize", "dump-ast", "dump-ir", "debug", "help":
		return true
	}
	return false
}

func parseLongOption(opts *cliOptions, name string) (cliAction, error) {
	switch name {
	case "version":
		return actionVersion, nil
	case "copyright":
		return actionCopyright, nil
	case "verbose":
		opts.verbose = true
	case "remove-lv":
		opts.removeLV = true
	case "no-ext-ops":
		opts.noExtOps = true
	case "no-optimize":
		opts.noOptimize = true
	case "dump-ast":
		opts.dumpAST = true
	case "dump-ir":
		opts.dumpIR = true
	case "debug":
		opts.debugInfo = true
	case "help":
		return actionCompile, errHelp
	default:
		return actionCompile, errHelp
	}
	return actionCompile, nil
}

func usage(w io.Writer, prog string) {
	fmt.Fprintf(w, "Usage: %s [switches] programfile...\n\nswitches:\n", prog)
	fmt.Fprint(w, `  -c           check syntax only
  -o<outfile>  place the output into <outfile> (- for stdout)
  -v           print version number, then turn on verbose mode
  -g           produce debugging information
  --remove-lv  remove local variables
  --no-ext-ops prohibit using OP_EXTs
  --no-optimize disable peephole optimization
  --verbose    print the generated instruction sequence
  --version    print the version
  --copyright  print the copyright
  --dump-ast   print the Prism AST as JSON
  --dump-ir    print the lowered IR
  --debug      alias for -g
  -h           print this help
`)
}
