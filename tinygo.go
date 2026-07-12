package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tinywasm/tinygo"
)

// tinygoWasmExecLocation is the shim's path relative to TINYGOROOT.
const tinygoWasmExecLocation = "targets/wasm_exec.js"

// tinygoWasmExecJS returns TinyGo's JS glue.
//
// TinyGo's wasm_exec.js is NOT interchangeable with the Go toolchain's: the two
// compilers emit different host-import signatures, so serving GOROOT's shim to a
// TinyGo binary fails at instantiation with an opaque link error. It ships inside
// TINYGOROOT, which is why the toolchain has to be located first.
func tinygoWasmExecJS() ([]byte, error) {
	root, err := tinygoRoot()
	if err != nil {
		return nil, err
	}

	shim := filepath.Join(root, filepath.FromSlash(tinygoWasmExecLocation))
	buf, err := os.ReadFile(shim)
	if err != nil {
		return nil, fmt.Errorf("tinygo: cannot read %s: %w", shim, err)
	}
	return buf, nil
}

// tinygoRoot resolves TINYGOROOT by asking the installed toolchain.
func tinygoRoot() (string, error) {
	bin, err := tinygo.GetPath()
	if err != nil {
		return "", fmt.Errorf("tinygo is not installed: %w\n"+
			"install it with: go run github.com/tinywasm/tinygo/cmd/tinygoinstall@latest", err)
	}

	out, err := exec.Command(bin, "env", "TINYGOROOT").Output()
	if err != nil {
		return "", fmt.Errorf("tinygo env TINYGOROOT failed: %w", err)
	}

	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("tinygo env TINYGOROOT returned an empty path")
	}
	return root, nil
}
