package wasm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/registry/remote"
)

const wasmMediaType = "application/vnd.wasm.content.layer.v1+wasm"

// LoadModule loads a Wasm module from an OCI reference or a local file path.
// References containing a "/" are treated as OCI; otherwise as local paths.
func LoadModule(ctx context.Context, ref string, plainHTTP bool) ([]byte, error) {
	if isLocalPath(ref) {
		return os.ReadFile(ref)
	}
	return pullFromOCI(ctx, ref, plainHTTP)
}

func isLocalPath(ref string) bool {
	if strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, ".") {
		return true
	}
	if _, err := os.Stat(ref); err == nil {
		return true
	}
	return false
}

func pullFromOCI(ctx context.Context, ref string, plainHTTP bool) ([]byte, error) {
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("parsing OCI reference %q: %w", ref, err)
	}
	repo.PlainHTTP = plainHTTP

	cacheDir, err := wasmCacheDir()
	if err != nil {
		return nil, fmt.Errorf("creating cache directory: %w", err)
	}

	store, err := file.New(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("creating file store at %q: %w", cacheDir, err)
	}
	defer store.Close()

	tag := tagFromRef(ref)
	desc, err := oras.Copy(ctx, repo, tag, store, tag, oras.DefaultCopyOptions)
	if err != nil {
		return nil, fmt.Errorf("pulling %q: %w", ref, err)
	}

	// The pulled artifact lands as a file named by its digest in the cache dir.
	digestFile := filepath.Join(cacheDir, desc.Digest.Encoded())
	data, err := os.ReadFile(digestFile)
	if err != nil {
		return nil, fmt.Errorf("reading cached module %q: %w", digestFile, err)
	}
	return data, nil
}

func tagFromRef(ref string) string {
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		return ref[i+1:]
	}
	return "latest"
}

func wasmCacheDir() (string, error) {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".cache")
	}
	cacheDir := filepath.Join(dir, "llm-d", "wasm")
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return "", err
	}
	return cacheDir, nil
}
