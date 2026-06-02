package output

import (
	"bufio"
	"os"
	"sync"
)

type Writer struct {
	mu sync.Mutex
	f  *os.File
	w  *bufio.Writer
}

func New(path string) (*Writer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &Writer{f: f, w: bufio.NewWriterSize(f, 4096)}, nil
}

func (w *Writer) Write(ip string) {
	w.mu.Lock()
	w.w.WriteString(ip + "\n") //nolint:errcheck
	w.mu.Unlock()
}

func (w *Writer) Flush() {
	w.mu.Lock()
	w.w.Flush() //nolint:errcheck
	w.mu.Unlock()
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.w.Flush() //nolint:errcheck
	return w.f.Close()
}
