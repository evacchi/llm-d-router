package main

import (
	"github.com/llm-d/llm-d-router/sdk/guest"
)

func init() {
	guest.RegisterFilter(func(req guest.ABIRequest, eps []guest.ABIEndpoint) []string {
		var keep []string
		for _, ep := range eps {
			if ep.Labels["gpu-type"] == "a100" {
				keep = append(keep, ep.ID)
			}
		}
		return keep
	})
}

func main() {}
