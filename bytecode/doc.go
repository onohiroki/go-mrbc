// Package bytecode implements the mruby 4.0.0 bytecode model and the RITE
// 04.00 container format.
//
// The package is the shared contract between go-mrbc and other tools such as a
// pure-Go mruby VM. It deliberately has no dependency on the parser or the
// compiler.
package bytecode
