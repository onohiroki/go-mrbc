// Package conformance embeds the shared Ruby behaviour corpus used by
// go-mrbc and go-mrbvm.
//
// Each case is a directory under FS containing main.rb, stdout.txt, and
// optionally status.txt, stderr.txt, and args.txt. Keeping the corpus in one
// module prevents the compiler and the VM from drifting apart.
package conformance

import (
	"embed"
	"io/fs"
)

//go:embed testdata/conformance
var embedded embed.FS

// FS is the conformance corpus rooted at the case directories.
var FS fs.FS

// Cases returns the sorted names of the available conformance cases.
func Cases() ([]string, error) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

func init() {
	sub, err := fs.Sub(embedded, "testdata/conformance")
	if err != nil {
		panic("conformance: " + err.Error())
	}
	FS = sub
}
