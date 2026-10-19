package realtime

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/lib/pq"
	"go.uber.org/zap"
)

// StartActivityNotifications listens on a dedicated connection, not the query
// pool. PostgreSQL delivers NOTIFY only on commit. A lost LISTEN connection
// invalidates WS subscriptions instead of silently losing the final update.
func StartActivityNotifications(ctx context.Context, dsn string, repo *storage.ActivityRepo, hub *Hub) (func(), error) {
	channel, err := repo.Channel(ctx)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	listener := pq.NewListener(dsn, time.Second, 30*time.Second, func(event pq.ListenerEventType, err error) {
		if event == pq.ListenerEventDisconnected || event == pq.ListenerEventConnectionAttemptFailed {
			hub.SetActivityAvailable(false)
		}
	})
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-done:
		}
	}()
	if err := listener.Listen(channel); err != nil {
		cancel()
		close(done)
		_ = listener.Close()
		return nil, err
	}
	hub.SetActivityAvailable(true)
	go func() {
		defer close(done)
		defer listener.Close()
		defer hub.SetActivityAvailable(false)
		ping := time.NewTicker(30 * time.Second)
		defer ping.Stop()
		pending := make(map[uuid.UUID]struct{})
		var flush <-chan time.Time
		var flushTimer *time.Timer
		defer func() {
			if flushTimer != nil {
				flushTimer.Stop()
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case notification, ok := <-listener.Notify:
				if !ok {
					return
				}
				if notification == nil {
					hub.SetActivityAvailable(false)
					hub.SetActivityAvailable(true)
					continue
				}
				if id, err := uuid.Parse(notification.Extra); err == nil && hub.HasSubscribers(id) {
					pending[id] = struct{}{}
					if flush == nil {
						flushTimer = time.NewTimer(25 * time.Millisecond)
						flush = flushTimer.C
					}
				}
			case <-flush:
				flush = nil
				for id := range pending {
					delete(pending, id)
					if !hub.HasSubscribers(id) {
						continue
					}
					readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					snapshot, err := repo.Snapshot(readCtx, id, "")
					cancel()
					if err != nil {
						logging.Named("activity").Warn("活动快照推送失败，重新建立订阅", zap.Error(err))
						hub.InvalidateConversation(id)
						continue
					}
					hub.Broadcast(id, Event{Type: TypeActivityUpdated, ConversationID: id.String(), BranchEpoch: snapshot.BranchEpoch,
						ActivityVersion: snapshot.ActivityVersion, Snapshot: snapshot, Reason: "stable_state_changed"})
				}
			case <-ping.C:
				if err := listener.Ping(); err != nil {
					hub.SetActivityAvailable(false)
				}
			}
		}
	}()
	return func() { cancel(); _ = listener.Close(); <-done }, nil
}
