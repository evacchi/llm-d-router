TINYGO ?= tinygo

EXAMPLES := label-filter queue-scorer
WASM_OUT  := $(addsuffix .wasm,$(EXAMPLES))

.PHONY: all clean

all: $(WASM_OUT)

%.wasm: %/main.go
	$(TINYGO) build -o $@ -target=wasip1 -buildmode=c-shared -no-debug ./$*

clean:
	rm -f $(WASM_OUT)
