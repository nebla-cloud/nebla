package osimages_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hexeption/nebla/internal/catalog"
	"github.com/Hexeption/nebla/internal/osimages"
	"github.com/Hexeption/nebla/internal/pve"
	"github.com/stretchr/testify/require"
)

// barrier holds armed NodeVersion calls until want are in flight together; delay slows given nodes always.
type barrier struct {
	want    int
	delay   map[string]time.Duration
	armed   atomic.Bool
	mu      sync.Mutex
	n       int
	all     chan struct{}
	timeout atomic.Bool
}

func (b *barrier) install(o *catalog.Options) {
	b.all = make(chan struct{})
	next := o.NewClient
	if next == nil {
		next = pve.New
	}
	o.NewClient = func(c pve.Conn) pve.Client { return barrierClient{next(c), b} }
}

func (b *barrier) arm()           { b.armed.Store(true) }
func (b *barrier) timedOut() bool { return b.timeout.Load() }

type barrierClient struct {
	pve.Client
	b *barrier
}

func (c barrierClient) NodeVersion(ctx context.Context, node string) (string, error) {
	if d := c.b.delay[node]; d > 0 {
		time.Sleep(d)
	}
	if c.b.armed.Load() {
		c.b.mu.Lock()
		c.b.n++
		if c.b.n == c.b.want {
			close(c.b.all)
		}
		c.b.mu.Unlock()
		select {
		case <-c.b.all:
		case <-time.After(3 * time.Second):
			c.b.timeout.Store(true)
		}
	}
	return c.Client.NodeVersion(ctx, node)
}

func TestListChecksConcurrently(t *testing.T) {
	b := &barrier{want: 4}
	e := buildEnvWith(t, false, b.install)
	l1, l2 := e.location("eu-1", "local-lvm", "imp"), e.location("eu-2", "local-lvm", "imp")
	b.arm()
	list, err := e.svc.List(context.Background())
	require.NoError(t, err)
	require.False(t, b.timedOut())
	require.True(t, locState(t, list, "debian-13", l1).Available)
	require.True(t, locState(t, list, "debian-13", l2).Available)
}

func TestAvailabilityKeepsNodeOrder(t *testing.T) {
	b := &barrier{delay: map[string]time.Duration{"pve1": 200 * time.Millisecond}}
	e := buildEnvWith(t, false, b.install)
	loc := e.location("eu-1", "ceph", "imp")
	l, err := e.cat.GetLocation(context.Background(), loc)
	require.NoError(t, err)
	require.Equal(t, []string{"pve1"}, osimages.BuildNodes(e.svc, l))
}
