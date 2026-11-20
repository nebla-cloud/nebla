package ext_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nebla-cloud/nebla/internal/ext"
	"github.com/stretchr/testify/require"
)

type recSink struct {
	mu  sync.Mutex
	got []ext.Event
	err error
}

func (s *recSink) Handle(_ context.Context, e ext.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, e)
	return s.err
}

func (s *recSink) events() []ext.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ext.Event(nil), s.got...)
}

func TestBusDeliversToAllSinks(t *testing.T) {
	a, b := &recSink{}, &recSink{}
	bus := ext.NewBus(a, b)
	e := ext.Event{Kind: ext.ServerCreated, ProjectID: 1, ServerID: 2, At: time.Now()}
	bus.Publish(context.Background(), e)
	bus.Close()
	require.Equal(t, []ext.Event{e}, a.events())
	require.Equal(t, []ext.Event{e}, b.events())
}

func TestBusSinkErrorDoesNotBlock(t *testing.T) {
	bad := &recSink{err: errors.New("boom")}
	good := &recSink{}
	bus := ext.NewBus(bad, good)
	bus.Publish(context.Background(), ext.Event{Kind: ext.ServerDeleted})
	bus.Close()
	require.Len(t, bad.events(), 1)
	require.Len(t, good.events(), 1)
}

func TestBusPublishDoesNotBlockOnSlowSink(t *testing.T) {
	release := make(chan struct{})
	slow := &blockSink{release: release}
	bus := ext.NewBus(slow)
	done := make(chan struct{})
	go func() {
		bus.Publish(context.Background(), ext.Event{Kind: ext.ServerUsage})
		close(done)
	}()
	<-done
	close(release)
	bus.Close()
	require.True(t, slow.called.Load())
}

func TestPublishAfterCloseIsDropped(t *testing.T) {
	s := &recSink{}
	bus := ext.NewBus(s)
	bus.Close()
	bus.Publish(context.Background(), ext.Event{Kind: ext.ServerCreated})
	bus.Close()
	require.Empty(t, s.events())
}

func TestDefaultRegistrationReadsSetting(t *testing.T) {
	d := ext.Defaults(staticSettings{"self_signup": true})
	require.True(t, d.SelfSignupEnabled(context.Background()))
	require.NoError(t, d.OnUserCreated(context.Background(), 1))
	require.False(t, ext.Defaults(nil).SelfSignupEnabled(context.Background()))
	require.False(t, ext.Defaults(staticSettings{}).SelfSignupEnabled(context.Background()))
}

type panicSink struct{}

func (panicSink) Handle(context.Context, ext.Event) error { panic("sink exploded") }

func TestBusRecoversFromSinkPanic(t *testing.T) {
	good := &recSink{}
	bus := ext.NewBus(panicSink{}, good)
	bus.Publish(context.Background(), ext.Event{Kind: ext.ServerCreated})
	bus.Close()
	require.Len(t, good.events(), 1)
}
