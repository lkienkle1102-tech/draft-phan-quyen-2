package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestServeShutsDownWhenContextIsCanceled(t *testing.T) {
	listener := newBlockingListener()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, &http.Server{Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}, listener)
	}()
	<-listener.accepted
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve() did not shut down")
	}
}

type blockingListener struct {
	accepted  chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{accepted: make(chan struct{}), closed: make(chan struct{})}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	l.closeOnce.Do(func() { close(l.accepted) })
	<-l.closed
	return nil, errors.New("listener closed")
}

func (l *blockingListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (*blockingListener) Addr() net.Addr { return fakeAddress("test") }

type fakeAddress string

func (a fakeAddress) Network() string { return string(a) }
func (a fakeAddress) String() string  { return string(a) }
