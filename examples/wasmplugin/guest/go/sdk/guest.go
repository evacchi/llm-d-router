package guest

import "unsafe"

// buf is the global buffer used for host-guest data exchange.
// The bump allocator hands out regions within this buffer.
var buf [1 << 20]byte // 1 MiB
var offset uint32

//go:wasmimport env log_message
func logMessage(ptr, size uint32)

// LogMessage sends a log line to the host logger.
func LogMessage(msg string) {
	if len(msg) == 0 {
		return
	}
	ptr := unsafe.Pointer(unsafe.StringData(msg))
	logMessage(uint32(uintptr(ptr)), uint32(len(msg)))
}

// Alloc reserves size bytes in the global buffer and returns a pointer.
// The buffer resets on each guest function call entry.
//
//export alloc
func Alloc(size uint32) *byte {
	if offset+size > uint32(len(buf)) {
		return nil
	}
	ptr := &buf[offset]
	offset += size
	return ptr
}

// resetBuf resets the bump allocator for a new call.
func resetBuf() {
	offset = 0
}

// readInput copies the host-written bytes from the buffer at the given pointer.
func readInput(ptr *byte, size uint32) []byte {
	return unsafe.Slice(ptr, size)
}

// writeOutput writes data to the buffer and returns the packed (ptr, len) as uint64.
func writeOutput(data []byte) uint64 {
	size := uint32(len(data))
	outPtr := Alloc(size)
	if outPtr == nil {
		return 0
	}
	copy(unsafe.Slice(outPtr, size), data)
	return uint64(uintptr(unsafe.Pointer(outPtr)))<<32 | uint64(size)
}
