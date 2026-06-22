package main

import (
	"github.com/llm-d/llm-d-router/examples/wasmplugin/guest/go/sdk"
)

func init() {
	guest.RegisterScorer(func(req guest.ABIRequest, eps []guest.ABIEndpoint) map[string]float64 {
		scores := make(map[string]float64, len(eps))
		var maxQ int
		for _, ep := range eps {
			if ep.Metrics.WaitingQueueSize > maxQ {
				maxQ = ep.Metrics.WaitingQueueSize
			}
		}
		for _, ep := range eps {
			if maxQ == 0 {
				scores[ep.ID] = 1.0
			} else {
				scores[ep.ID] = 1.0 - float64(ep.Metrics.WaitingQueueSize)/float64(maxQ)
			}
		}
		return scores
	})
}

func main() {}
