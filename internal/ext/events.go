package ext

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type EventKind string

const (
	ServerCreated     EventKind = "server.created"
	ServerDeleted     EventKind = "server.deleted"
	ServerTypeChanged EventKind = "server.type_changed"
	ServerUsage       EventKind = "server.usage"
	VolumeCreated     EventKind = "volume.created"
	VolumeResized     EventKind = "volume.resized"
	VolumeDeleted     EventKind = "volume.deleted"
	VolumeUsage       EventKind = "volume.usage"
)

type Event struct {
	Kind      EventKind
	ProjectID int64
	// ServerID is set on server events for compatibility; ResourceType and ResourceID name the resource.
	ServerID     int64
	ResourceType string
	ResourceID   int64
	At           time.Time
	Data         map[string]any
}

// Bus fans events out to sinks asynchronously.
type Bus struct {
	sinks  []BillingSink
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
}

func NewBus(sinks ...BillingSink) *Bus { return &Bus{sinks: sinks} }

// Publish delivers e to every sink without blocking; sink errors are logged.
func (b *Bus) Publish(ctx context.Context, e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	ctx = context.WithoutCancel(ctx)
	for _, s := range b.sinks {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("event sink panicked", "kind", e.Kind, "server_id", e.ServerID, "panic", r)
				}
			}()
			if err := s.Handle(ctx, e); err != nil {
				slog.Error("event sink failed", "kind", e.Kind, "server_id", e.ServerID, "err", err)
			}
		}()
	}
}

// Close stops accepting events and waits for in-flight deliveries.
func (b *Bus) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.wg.Wait()
}
