package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
)

// LoadModule loads a Wasm module from an OCI reference or a local file path.
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

	tag := tagFromRef(ref)

	manifestDesc, err := repo.Resolve(ctx, tag)
	if err != nil {
		return nil, fmt.Errorf("resolving %q: %w", ref, err)
	}

	manifestRC, err := repo.Fetch(ctx, manifestDesc)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest for %q: %w", ref, err)
	}
	manifestBytes, err := io.ReadAll(manifestRC)
	manifestRC.Close()
	if err != nil {
		return nil, fmt.Errorf("reading manifest for %q: %w", ref, err)
	}

	var manifest ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest for %q: %w", ref, err)
	}

	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("no layers in manifest for %q", ref)
	}

	layerDesc := manifest.Layers[0]
	layerRC, err := repo.Fetch(ctx, layerDesc)
	if err != nil {
		return nil, fmt.Errorf("fetching wasm layer from %q: %w", ref, err)
	}
	defer layerRC.Close()

	return io.ReadAll(layerRC)
}

func tagFromRef(ref string) string {
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		return ref[i+1:]
	}
	return "latest"
}
