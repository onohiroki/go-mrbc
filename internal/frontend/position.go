package frontend

import (
	"fmt"
	"sort"
)

// Position is a one-based source location.
type Position struct {
	Offset int
	Line   int
	Column int
}

// Source maps byte offsets to line and column numbers.
type Source struct {
	File   string
	text   []byte
	starts []int
}

// NewSource records the line starts of text.
func NewSource(file string, text []byte) *Source {
	starts := []int{0}
	for i, b := range text {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &Source{File: file, text: text, starts: starts}
}

// Position converts a byte offset into a Position.
func (s *Source) Position(offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(s.text) {
		offset = len(s.text)
	}
	index := sort.Search(len(s.starts), func(i int) bool { return s.starts[i] > offset }) - 1
	if index < 0 {
		index = 0
	}
	return Position{Offset: offset, Line: index + 1, Column: offset - s.starts[index] + 1}
}

// Error is a compile error with a source position.
type Error struct {
	File     string
	Position Position
	Message  string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Position.Line, e.Position.Column, e.Message)
}

func (s *Source) errorf(offset int, format string, args ...any) error {
	return &Error{
		File:     s.File,
		Position: s.Position(offset),
		Message:  fmt.Sprintf(format, args...),
	}
}
