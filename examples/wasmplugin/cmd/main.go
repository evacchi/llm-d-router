// Package main demonstrates how to build an out-of-tree EPP with
// wasm-filter and wasm-scorer plugins compiled in.
package main

import (
	"os"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/llm-d/llm-d-router/cmd/epp/runner"
	"github.com/llm-d/llm-d-router/examples/wasmplugin/host"
	"github.com/llm-d/llm-d-router/pkg/epp/framework/interface/plugin"
)

func init() {
	plugin.Register(host.WasmFilterType, host.WasmFilterFactory)
	plugin.Register(host.WasmScorerType, host.WasmScorerFactory)
}

func main() {
	os.Exit(run())
}

func run() int {
	ctx := ctrl.SetupSignalHandler()
	if err := runner.NewRunner().Run(ctx); err != nil {
		return 1
	}
	return 0
}
