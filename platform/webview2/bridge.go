package webview2

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// BridgeMessage is one page-to-host message. Payload is preserved as JSON so
// reporters can decode only the message kinds they understand.
type BridgeMessage struct {
	RealmID string          `json:"realmId"`
	Payload json.RawMessage `json:"payload"`
}

// BridgeBatch is the unit delivered across the adapter boundary.
type BridgeBatch struct {
	Sequence uint64          `json:"sequence"`
	Messages []BridgeMessage `json:"messages"`
}

type bridgeBatcher struct {
	ctx       context.Context
	cancel    context.CancelFunc
	in        chan BridgeMessage
	out       chan BridgeBatch
	done      chan struct{}
	maxCount  int
	maxBytes  int
	interval  time.Duration
	closeOnce sync.Once
}

func newBridgeBatcher(parent context.Context, count, bytes int, interval time.Duration) *bridgeBatcher {
	ctx, cancel := context.WithCancel(parent)
	b := &bridgeBatcher{
		ctx: ctx, cancel: cancel, in: make(chan BridgeMessage, count*2),
		out: make(chan BridgeBatch, 16), done: make(chan struct{}),
		maxCount: count, maxBytes: bytes, interval: interval,
	}
	go b.run()
	return b
}

func (b *bridgeBatcher) push(ctx context.Context, message BridgeMessage) error {
	select {
	case b.in <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return errors.New("webview2: bridge is closed")
	}
}

func (b *bridgeBatcher) close() {
	b.closeOnce.Do(b.cancel)
	<-b.done
}

func (b *bridgeBatcher) run() {
	defer close(b.done)
	defer close(b.out)
	timer := time.NewTimer(b.interval)
	if !timer.Stop() {
		<-timer.C
	}
	var messages []BridgeMessage
	var size int
	var sequence uint64
	flush := func() {
		if len(messages) == 0 {
			return
		}
		sequence++
		batch := BridgeBatch{Sequence: sequence, Messages: messages}
		select {
		case b.out <- batch:
		case <-b.ctx.Done():
			return
		}
		messages = nil
		size = 0
	}
	for {
		select {
		case message := <-b.in:
			messageSize := len(message.RealmID) + len(message.Payload)
			if len(messages) > 0 && (len(messages) >= b.maxCount || size+messageSize > b.maxBytes) {
				flush()
			}
			messages = append(messages, message)
			size += messageSize
			if len(messages) == 1 {
				timer.Reset(b.interval)
			}
			if len(messages) >= b.maxCount || size >= b.maxBytes {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				flush()
			}
		case <-timer.C:
			flush()
		case <-b.ctx.Done():
			flush()
			return
		}
	}
}
