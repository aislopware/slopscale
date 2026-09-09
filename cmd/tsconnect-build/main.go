// Command tsconnect-build compiles Tailscale's in-browser client
// (cmd/tsconnect/wasm) for the console's SSH terminal and puts it where
// Vite serves it: web/public/tsconnect/main-<hash>.wasm, the same file
// gzipped for the embedded server, the Go runtime's wasm_exec.js and a
// manifest.json the console reads to find them. The raw file is only for
// the dev server; make web removes it from dist so the binary embeds the
// gzipped one.
//
// It runs under make wasm, which make web depends on.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"tailscale.com/cmd/tsconnect/wasmbuild"
)

// manifest is what the console fetches at /admin/tsconnect/manifest.json.
type manifest struct {
	// Wasm is the file name of the client, hashed so it caches forever.
	Wasm string `json:"wasm"`
	// ExecJS is the Go runtime's loader.
	ExecJS string `json:"execJs"`
	// SHA256 is the raw wasm's digest; a rebuild with the same bytes
	// leaves the files alone.
	SHA256 string `json:"sha256"`
}

const (
	execJS       = "wasm_exec.js"
	manifestFile = "manifest.json"
	hashLength   = 12
	filePerm     = 0o644
	dirPerm      = 0o755
)

func main() {
	out := flag.String("out", "web/public/tsconnect", "directory the files are written to")

	flag.Parse()

	err := run(*out)
	if err != nil {
		log.Fatal(err)
	}
}

func run(out string) error {
	err := os.MkdirAll(out, dirPerm)
	if err != nil {
		return fmt.Errorf("creating %s: %w", out, err)
	}

	raw, err := build()
	if err != nil {
		return err
	}

	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	name := "main-" + digest[:hashLength] + ".wasm"

	if current(out, digest) {
		fmt.Fprintf(os.Stderr, "tsconnect: %s is current (%d bytes)\n", name, len(raw))

		return nil
	}

	err = clean(out)
	if err != nil {
		return err
	}

	err = writeFile(filepath.Join(out, name), raw)
	if err != nil {
		return err
	}

	packed, err := gzipped(raw)
	if err != nil {
		return err
	}

	err = writeFile(filepath.Join(out, name+".gz"), packed)
	if err != nil {
		return err
	}

	err = copyExecJS(out)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(manifest{Wasm: name, ExecJS: execJS, SHA256: digest}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}

	err = writeFile(filepath.Join(out, manifestFile), append(data, '\n'))
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "tsconnect: built %s (%d bytes, %d gzipped)\n", name, len(raw), len(packed))

	return nil
}

// build compiles the client into a temporary file and returns its
// bytes. Tailscale's own build sets the tailscale_go tag for its Go
// fork; this one runs on the standard toolchain, so the tag is dropped
// and the rest of its tag list, which strips every feature a browser
// cannot use, is kept.
func build() ([]byte, error) {
	tmp, err := os.CreateTemp("", "tsconnect-*.wasm")
	if err != nil {
		return nil, fmt.Errorf("creating temporary file: %w", err)
	}

	tmpPath := tmp.Name()

	err = tmp.Close()
	if err != nil {
		return nil, fmt.Errorf("closing temporary file: %w", err)
	}

	defer os.Remove(tmpPath)

	tags := slices.DeleteFunc(strings.Split(wasmbuild.Tags(), ","), func(t string) bool {
		return t == "tailscale_go"
	})

	cmd := exec.CommandContext(context.Background(), "go", "build", //nolint:gosec // a build step's own arguments
		"-tags", strings.Join(tags, ","),
		"-trimpath",
		"-ldflags", wasmbuild.ProdLDFlags(),
		"-o", tmpPath,
		"tailscale.com/cmd/tsconnect/wasm",
	)

	// goreleaser vendors the module before make web, and the client's
	// packages are not imported by the server, so they are not in vendor;
	// build from the module cache instead.
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0", "GOFLAGS=-mod=mod")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("building tsconnect wasm: %w", err)
	}

	raw, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("reading built wasm: %w", err)
	}

	return raw, nil
}

// current reports whether the manifest already names this build and its
// files exist.
func current(out, digest string) bool {
	data, err := os.ReadFile(filepath.Join(out, manifestFile))
	if err != nil {
		return false
	}

	var m manifest

	err = json.Unmarshal(data, &m)
	if err != nil || m.SHA256 != digest {
		return false
	}

	for _, f := range []string{m.Wasm, m.Wasm + ".gz", m.ExecJS} {
		_, err = os.Stat(filepath.Join(out, f))
		if err != nil {
			return false
		}
	}

	return true
}

// clean removes earlier builds so the directory holds one client.
func clean(out string) error {
	entries, err := os.ReadDir(out)
	if err != nil {
		return fmt.Errorf("reading %s: %w", out, err)
	}

	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "main-") || !strings.Contains(e.Name(), ".wasm") {
			continue
		}

		err = os.Remove(filepath.Join(out, e.Name()))
		if err != nil {
			return fmt.Errorf("removing %s: %w", e.Name(), err)
		}
	}

	return nil
}

// copyExecJS takes the loader from the toolchain that built the wasm,
// found through go env so it matches whichever Go is on PATH.
func copyExecJS(out string) error {
	goroot, err := exec.CommandContext(context.Background(), "go", "env", "GOROOT").Output()
	if err != nil {
		return fmt.Errorf("finding GOROOT: %w", err)
	}

	src := filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", execJS)

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading %s: %w", src, err)
	}

	return writeFile(filepath.Join(out, execJS), data)
}

func gzipped(raw []byte) ([]byte, error) {
	var buf bytes.Buffer

	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("creating gzip writer: %w", err)
	}

	_, err = zw.Write(raw)
	if err != nil {
		return nil, fmt.Errorf("compressing wasm: %w", err)
	}

	err = zw.Close()
	if err != nil {
		return nil, fmt.Errorf("finishing gzip: %w", err)
	}

	return buf.Bytes(), nil
}

// writeFile writes a file the dev server and the embed both read; it is
// build output, not a secret, so it is world-readable like the rest of
// dist.
func writeFile(path string, data []byte) error {
	err := os.WriteFile(path, data, filePerm) //nolint:gosec // build output served to browsers
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
