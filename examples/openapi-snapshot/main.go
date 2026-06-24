// Command openapi-snapshot writes or checks a committed OpenAPI contract
// snapshot, demonstrating the contract-drift guard pattern. Wire the "check"
// mode into CI so a code change that alters the generated contract without
// updating the committed snapshot fails the build.
//
// Usage:
//
//	go run . write   # overwrite the snapshot (the default)
//	go run . check   # exit non-zero when the contract has drifted
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mrz1836/go-actions/examples/petstore"
)

// snapshotPath is the committed OpenAPI contract baseline, relative to the
// working directory.
const snapshotPath = "openapi.snapshot.json"

// errDrift signals that the generated contract no longer matches the snapshot.
var errDrift = errors.New("OpenAPI contract has drifted from the committed snapshot")

// errUnknownMode signals an unrecognized command-line mode.
var errUnknownMode = errors.New("unknown mode (want write or check)")

// run regenerates the OpenAPI document and either writes it to path or checks
// the committed snapshot at path against it, reporting progress to out.
func run(mode, path string, out io.Writer) error {
	reg := petstore.Registry()
	reg.Freeze()
	generated := reg.OpenAPIJSON()

	switch mode {
	case "write":
		if err := os.WriteFile(path, generated, 0o600); err != nil {
			return fmt.Errorf("write snapshot: %w", err)
		}
		_, _ = fmt.Fprintln(out, "OpenAPI snapshot written:", path)
		return nil
	case "check":
		committed, err := os.ReadFile(path) //nolint:gosec // path is a fixed in-repo snapshot
		if err != nil {
			return fmt.Errorf("read snapshot: %w", err)
		}
		if !bytes.Equal(committed, generated) {
			return errDrift
		}
		_, _ = fmt.Fprintln(out, "OpenAPI snapshot matches the generated contract.")
		return nil
	default:
		return errUnknownMode
	}
}

func main() {
	mode := "write"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if err := run(mode, snapshotPath, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
