package testenv

import (
	"context"
	"errors"
	"net"
	"sync"
)

// memListener is a net.Listener whose connections are in-memory pipes.
// Tests talk real HTTP over it without opening sockets, so they run the
// same in a locked-down sandbox as on a laptop.
type memListener struct {
	ch     chan net.Conn
	once   sync.Once
	closed chan struct{}
}

func newMemListener() *memListener {
	return &memListener{ch: make(chan net.Conn), closed: make(chan struct{})}
}

type memAddr struct{}

func (memAddr) Network() string { return "mem" }
func (memAddr) String() string  { return "127.0.0.1:1" }

func (l *memListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *memListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *memListener) Addr() net.Addr { return memAddr{} }

// Dial connects to the listener.
func (l *memListener) Dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.ch <- server:
		return client, nil
	case <-l.closed:
		return nil, errors.New("listener closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
