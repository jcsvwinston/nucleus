package nucleus

import (
	"bytes"
	"sync"
)

// syncBuffer serialises the logger's writes (manager goroutines) against the
// test's reads: a bare bytes.Buffer shared between the two is exactly the
// race -race catches.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
