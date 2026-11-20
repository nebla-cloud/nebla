package ext_test

import (
	"context"
	"sync/atomic"

	"github.com/nebla-cloud/nebla/internal/ext"
)

type blockSink struct {
	release chan struct{}
	called  atomic.Bool
}

func (s *blockSink) Handle(context.Context, ext.Event) error {
	<-s.release
	s.called.Store(true)
	return nil
}

type staticSettings map[string]bool

func (m staticSettings) Bool(_ context.Context, key string, def bool) bool {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}
