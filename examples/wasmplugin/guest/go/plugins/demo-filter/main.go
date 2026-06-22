package main

import (
	"github.com/llm-d/llm-d-router/examples/wasmplugin/guest/go/sdk"
)

// filterTier and filterVersion are set via build tags (see v1.go, v2.go).
var filterTier string
var filterVersion string

func init() {
	guest.RegisterFilter(customFilter)
}

func customFilter(req guest.ABIRequest, eps []guest.ABIEndpoint) []string {
	guest.LogMessage("wasm-filter " + filterVersion + ": keeping " + filterTier + "-tier endpoints")
	var keep []string
	for _, ep := range eps {
		if filterTier == "" || ep.Labels["tier"] == filterTier {
			keep = append(keep, ep.ID)
		}
	}
	return keep
}

func main() {}
