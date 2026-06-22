package guest

import "encoding/json"

// FilterFunc is the signature for guest filter implementations.
// It receives the request and candidate endpoints, and returns the IDs
// of the endpoints to keep.
type FilterFunc func(request ABIRequest, endpoints []ABIEndpoint) []string

var registeredFilter FilterFunc

// RegisterFilter registers a filter function to be called by the host.
// Call this from your main() or init().
func RegisterFilter(fn FilterFunc) {
	registeredFilter = fn
}

//export filter
func filter(ptr *byte, size uint32) uint64 {
	resetBuf()

	data := readInput(ptr, size)

	var input filterInput
	if err := json.Unmarshal(data, &input); err != nil {
		return 0
	}

	if registeredFilter == nil {
		return 0
	}

	ids := registeredFilter(input.Request, input.Endpoints)

	out, err := json.Marshal(filterOutput{EndpointIDs: ids})
	if err != nil {
		return 0
	}

	return writeOutput(out)
}
