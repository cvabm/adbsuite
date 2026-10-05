package procutil

import "sync"

// Output retains a bounded tail and is safe to read while a child is writing.
type Output struct {
	mu   sync.Mutex
	data []byte
}

const maxOutput = 64 * 1024

func (b *Output) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= maxOutput {
		b.data = append(b.data[:0], p[n-maxOutput:]...)
	} else {
		if len(b.data)+n > maxOutput {
			b.data = b.data[len(b.data)+n-maxOutput:]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (b *Output) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
