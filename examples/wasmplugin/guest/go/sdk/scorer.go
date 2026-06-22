package guest

import "encoding/json"

// ScoreFunc is the signature for guest scorer implementations.
// It receives the request and candidate endpoints, and returns a map
// of endpoint ID to score in [0, 1].
type ScoreFunc func(request ABIRequest, endpoints []ABIEndpoint) map[string]float64

var registeredScorer ScoreFunc

// RegisterScorer registers a scorer function to be called by the host.
// Call this from your main() or init().
func RegisterScorer(fn ScoreFunc) {
	registeredScorer = fn
}

//export score
func score(ptr *byte, size uint32) uint64 {
	resetBuf()

	data := readInput(ptr, size)

	var input scorerInput
	if err := json.Unmarshal(data, &input); err != nil {
		return 0
	}

	if registeredScorer == nil {
		return 0
	}

	scores := registeredScorer(input.Request, input.Endpoints)

	out, err := json.Marshal(scorerOutput{Scores: scores})
	if err != nil {
		return 0
	}

	return writeOutput(out)
}
