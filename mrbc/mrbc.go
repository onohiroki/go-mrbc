// Package mrbc compiles Ruby source to mruby 4.0.0 bytecode.
package mrbc

import (
	"context"
	"fmt"
	"os"

	"github.com/onohiroki/go-mrbc/bytecode"
	"github.com/onohiroki/go-mrbc/internal/compiler"
	"github.com/onohiroki/go-mrbc/internal/frontend"
)

// Version is the go-mrbc release version.
const Version = "0.1.0"

// Target describes the bytecode target.
const Target = "mruby 4.0.0 / RITE 04.00"

// Options controls compilation.
type Options struct {
	// FilePath is used for error messages and debug information.
	FilePath string
	// DebugInfo is reserved for the DBG section.
	DebugInfo bool
	// RemoveLV is reserved for suppressing the LVAR section.
	RemoveLV bool
	// NoOptimize is accepted for mrbc compatibility. Optimization is not
	// implemented yet, so this is currently the only behavior.
	NoOptimize bool
	// LocalNames declares extra local variables for the top-level scope. It
	// lets an evaluator compile source against the caller's locals.
	LocalNames []string
	// LineOffset shifts every reported line number. Evaluated source starts
	// at the line its caller passed instead of line 1.
	LineOffset int
	// Eval compiles evaluated source. A top-level return leaves the caller's
	// method, a forwarding super carries the caller's argument layout, and a
	// yield reads the caller's block.
	Eval bool
	// EvalInMethod says the evaluated source runs in a method and may yield.
	EvalInMethod bool
}

// Compile parses and compiles src into an irep.
func Compile(ctx context.Context, src []byte, opts Options) (*bytecode.Irep, error) {
	file := opts.FilePath
	if file == "" {
		file = "<string>"
	}
	var program *frontend.Program
	var err error
	if opts.Eval {
		program, err = frontend.LowerEvalWithLineOffset(ctx, src, file, opts.LineOffset)
	} else {
		program, err = frontend.LowerWithLineOffset(ctx, src, file, opts.LineOffset)
	}
	if err != nil {
		return nil, err
	}
	return compiler.CompileWithOptions(program, file, src, compiler.Options{
		ExtraLocals:  opts.LocalNames,
		Eval:         opts.Eval,
		EvalInMethod: opts.EvalInMethod,
	})
}

// CompileFile reads path, compiles it, and returns a RITE binary.
func CompileFile(ctx context.Context, path string, opts Options) ([]byte, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if opts.FilePath == "" {
		opts.FilePath = path
	}
	irep, err := Compile(ctx, src, opts)
	if err != nil {
		return nil, err
	}
	return DumpWithOptions(irep, bytecode.DumpOptions{
		DebugInfo: opts.DebugInfo,
		RemoveLV:  opts.RemoveLV,
	})
}

// Dump serializes an irep as a RITE 04.00 binary with default options.
func Dump(irep *bytecode.Irep) ([]byte, error) {
	return DumpWithOptions(irep, bytecode.DumpOptions{})
}

// DumpWithOptions serializes an irep as a RITE 04.00 binary.
func DumpWithOptions(irep *bytecode.Irep, opts bytecode.DumpOptions) ([]byte, error) {
	binary, err := bytecode.DumpBinaryWithOptions(irep, opts)
	if err != nil {
		return nil, fmt.Errorf("dump bytecode: %w", err)
	}
	return binary, nil
}
