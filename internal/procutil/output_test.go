package procutil

import (
	"strings"
	"sync"
	"testing"
)

func TestOutputBoundedTail(t *testing.T) {
	var output Output
	text := strings.Repeat("x", maxOutput*2) + "tail"
	if n, err := output.Write([]byte(text)); err != nil || n != len(text) {
		t.Fatal("short write")
	}
	if got := output.String(); len(got) != maxOutput || !strings.HasSuffix(got, "tail") {
		t.Fatal("incorrect bounded output")
	}
}

func TestOutputConcurrentReadWrite(t *testing.T) {
	var output Output
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				_, _ = output.Write([]byte("message"))
				_ = output.String()
			}
		}()
	}
	workers.Wait()
}
