// Wrapper for gopatch --print-only: applies each patch to each input file
// and writes the result to the output directory.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	gopatchBin := flag.String("gopatch", "", "path to gopatch binary")
	outDir := flag.String("o", "", "output directory")
	var patches []string
	flag.Func("p", "gopatch patch file", func(s string) error {
		patches = append(patches, s)
		return nil
	})
	flag.Parse()

	if *gopatchBin == "" || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: run_gopatch -gopatch <bin> -o <outdir> [-p <patch>]... <file>...")
		os.Exit(1)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, infile := range flag.Args() {
		// filepath.Walk (used by gopatch) does not follow symlinks; resolve
		// them so gopatch can find the file (Bazel inputs are symlinks).
		if resolved, err := filepath.EvalSymlinks(infile); err == nil {
			infile = resolved
		}

		gopatchArgs := []string{"--print-only"}
		for _, p := range patches {
			gopatchArgs = append(gopatchArgs, "-p", p)
		}
		gopatchArgs = append(gopatchArgs, infile)

		cmd := exec.Command(*gopatchBin, gopatchArgs...)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopatch failed on %s: %v\n", infile, err)
			os.Exit(1)
		}

		outfile := filepath.Join(*outDir, filepath.Base(infile))
		if err := os.WriteFile(outfile, out, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
