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
const Target = "mruby 4.0.0 (RITE 04.00)"

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
}

// Compile parses and compiles src into an irep.
func Compile(ctx context.Context, src []byte, opts Options) (*bytecode.Irep, error) {
	file := opts.FilePath
	if file == "" {
		file = "<string>"
	}
	program, err := frontend.Lower(ctx, src, file)
	if err != nil {
		return nil, err
	}
	return compiler.Compile(program, file, src)
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
	return Dump(irep)
}

// Dump serializes an irep as a RITE 04.00 binary.
func Dump(irep *bytecode.Irep) ([]byte, error) {
	binary, err := bytecode.DumpBinary(irep)
	if err != nil {
		return nil, fmt.Errorf("dump bytecode: %w", err)
	}
	return binary, nil
}
