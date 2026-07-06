package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// captureStdout redirects the real os.Stdout to a pipe for the duration of
// the test and returns a function that restores it and returns everything
// written. Needed because handleEvent's console relay in main.go writes
// directly via fmt.Printf to os.Stdout, not to any io.Writer callers pass in.
func captureStdout(t *testing.T) func() []byte {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	return func() []byte {
		os.Stdout = orig
		w.Close()
		out, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("failed to read captured stdout: %v", err)
		}
		return out
	}
}

func TestRun(t *testing.T) {
	t.Skip("Too flaky. See: https://github.com/agnivade/wasmbrowsertest/issues/59")
	for _, tc := range []struct {
		description string
		files       map[string]string
		args        []string
		expectErr   string
	}{
		{
			description: "handle panic",
			files: map[string]string{
				"go.mod": `
module foo

go 1.20
`,
				"foo.go": `
package main

func main() {
	panic("failed")
}
`,
			},
			expectErr: "exit with status 2",
		},
		{
			description: "handle panic in next run of event loop",
			files: map[string]string{
				"go.mod": `
		module foo

		go 1.20
		`,
				"foo.go": `
		package main

		import (
			"syscall/js"
		)

		func main() {
			js.Global().Call("setTimeout", js.FuncOf(func(js.Value, []js.Value) any {
				panic("bad")
				return nil
			}), 0)
		}
		`,
			},
			expectErr: "",
		},
		{
			description: "handle callback after test exit",
			files: map[string]string{
				"go.mod": `
		module foo

		go 1.20
		`,
				"foo.go": `
		package main

		import (
			"syscall/js"
			"fmt"
		)

		func main() {
			js.Global().Call("setInterval", js.FuncOf(func(js.Value, []js.Value) any {
				fmt.Println("callback")
				return nil
			}), 5)
			fmt.Println("done")
		}
		`,
			},
			expectErr: "",
		},
	} {
		t.Run(tc.description, func(t *testing.T) {
			dir := t.TempDir()
			for fileName, contents := range tc.files {
				writeFile(t, dir, fileName, contents)
			}
			wasmFile := buildTestWasm(t, dir)
			_, err := testRun(t, wasmFile, tc.args...)
			assertEqualError(t, tc.expectErr, err)
		})
	}
}

// TestRunConsoleRelay is a regression test for the devbrowser migration
// (see docs/CHECK_PLAN.md): it exercises the real chromedp/console-relay
// code path in main.go's run() — allocator setup via
// devbrowser.ResolveChromeExecPath, chromedp.ListenTarget/handleEvent, and
// exit-code propagation — end-to-end against a real browser. Unlike
// TestRun above, this isn't skipped: it's the permanent, in-library
// coverage for the exact bug this migration fixed (Chrome resolution) and
// the exact mechanism gotest depends on (console output relay).
func TestRunConsoleRelay(t *testing.T) {
	t.Run("passing test relays console output and exits 0", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", "module fixture\n\ngo 1.20\n")
		writeFile(t, dir, "fixture.go", `
package main

import "fmt"

func main() {
	fmt.Println("hello from wasm fixture")
}
`)
		wasmFile := buildTestWasm(t, dir)

		// The console relay (handleEvent in main.go) writes directly to the
		// real os.Stdout via fmt.Printf, not to the io.Writer passed into
		// run(). Capture real stdout to observe it.
		stdout := captureStdout(t)
		_, err := testRun(t, wasmFile)
		out := stdout()
		if err != nil {
			t.Fatalf("unexpected error: %v\nstdout:\n%s", err, out)
		}
		if !bytes.Contains(out, []byte("hello from wasm fixture")) {
			t.Errorf("expected console output to be relayed, got stdout:\n%s", out)
		}
	})

	t.Run("panicking test propagates a non-zero exit", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "go.mod", "module fixture\n\ngo 1.20\n")
		writeFile(t, dir, "fixture.go", `
package main

func main() {
	panic("deliberate failure for regression test")
}
`)
		wasmFile := buildTestWasm(t, dir)
		_, err := testRun(t, wasmFile)
		if err == nil {
			t.Fatal("expected a non-nil error from a panicking wasm binary, got nil")
		}
	})
}

func testRun(t *testing.T, wasmFile string, flags ...string) ([]byte, error) {
	var logs bytes.Buffer
	flagSet := flag.NewFlagSet("wasmbrowsertest", flag.ContinueOnError)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	err := run(ctx, append([]string{"go_js_wasm_exec", wasmFile}, flags...), &logs, flagSet)
	return logs.Bytes(), err
}

// writeFile creates a file at $baseDir/$path with the given contents, where 'path' is slash separated
func writeFile(t *testing.T, baseDir, path, contents string) {
	t.Helper()
	path = filepath.FromSlash(path)
	fullPath := filepath.Join(baseDir, path)
	err := os.MkdirAll(filepath.Dir(fullPath), 0755)
	if err != nil {
		t.Fatal("Failed to create file's base directory:", err)
	}
	err = os.WriteFile(fullPath, []byte(contents), 0600)
	if err != nil {
		t.Fatal("Failed to create file:", err)
	}
}

// buildTestWasm builds the given Go package's test binary and returns the output Wasm file
func buildTestWasm(t *testing.T, path string) string {
	t.Helper()
	outputFile := filepath.Join(t.TempDir(), "out.wasm")
	cmd := exec.Command("go", "build", "-o", outputFile, ".")
	cmd.Dir = path
	cmd.Env = append(os.Environ(),
		"GOOS=js",
		"GOARCH=wasm",
	)
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		t.Log(string(output))
	}
	if err != nil {
		t.Fatal("Failed to build Wasm binary:", err)
	}
	return outputFile
}

func assertEqualError(t *testing.T, expected string, err error) {
	t.Helper()
	if expected == "" {
		if err != nil {
			t.Error("Unexpected error:", err)
		}
		return
	}

	if err == nil {
		t.Error("Expected error, got nil")
		return
	}
	message := err.Error()
	if expected != message {
		t.Errorf("Unexpected error message: %q != %q", expected, message)
	}
}
