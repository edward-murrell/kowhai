package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
)

// Mem is an in-memory store for tests.
type Mem struct {
	mu   sync.Mutex
	Objs map[string][]byte
}

func NewMem() *Mem { return &Mem{Objs: map[string][]byte{}} }

func (m *Mem) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.Objs[key] = b
	m.mu.Unlock()
	return nil
}

func (m *Mem) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	b, ok := m.Objs[key]
	m.mu.Unlock()
	if !ok {
		return nil, errors.New("no such key")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *Mem) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.Objs, key)
	m.mu.Unlock()
	return nil
}

func (m *Mem) Ping(context.Context) error { return nil }
