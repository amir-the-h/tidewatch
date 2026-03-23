package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

const channelBufferSize = 256

// Dispatcher listens for NotificationEvents and dispatches Pushover messages
// to all matching active notification configurations.
type Dispatcher struct {
	ch     chan NotificationEvent
	store  store.Store
	client *PushoverClient
	logger *slog.Logger
}

// NewDispatcher creates a new Dispatcher. It satisfies the EventEmitter interface.
func NewDispatcher(s store.Store, client *PushoverClient, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		ch:     make(chan NotificationEvent, channelBufferSize),
		store:  s,
		client: client,
		logger: logger,
	}
}

// Emit sends an event to the dispatcher channel. It is non-blocking: if the
// channel is full the event is dropped and a warning is logged. Safe for
// concurrent use.
func (d *Dispatcher) Emit(event NotificationEvent) {
	select {
	case d.ch <- event:
	default:
		d.logger.Warn("notifier: dispatcher: channel full, dropping event",
			"event_type", event.Type)
	}
}

// Start runs the dispatch loop until ctx is cancelled, then drains remaining
// events before returning.
func (d *Dispatcher) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.drain()
			return
		case event := <-d.ch:
			d.handle(ctx, event)
		}
	}
}

// drain processes all events currently buffered in the channel using
// context.Background() so they can complete even after the parent context
// has been cancelled.
func (d *Dispatcher) drain() {
	for {
		select {
		case event := <-d.ch:
			d.handle(context.Background(), event)
		default:
			return
		}
	}
}

// handle queries the store for matching notification configs and sends
// Pushover messages for each one that passes filter checks.
func (d *Dispatcher) handle(ctx context.Context, event NotificationEvent) {
	active := true
	cfgs, err := d.store.ListNotifications(ctx, store.NotificationFilter{
		EventType: &event.Type,
		Active:    &active,
	})
	if err != nil {
		d.logger.Error("notifier: dispatcher: list notifications",
			"error", err, "event_type", event.Type)
		return
	}

	for _, cfg := range cfgs {
		if !matchesFilters(cfg, event) {
			continue
		}

		msg, err := buildMessage(cfg, event)
		if err != nil {
			d.logger.Error("notifier: dispatcher: build message",
				"error", err, "notification_id", cfg.ID)
			continue
		}

		if err := d.client.Send(ctx, msg); err != nil {
			d.logger.Error("notifier: dispatcher: send pushover",
				"error", err, "notification_id", cfg.ID)
			continue
		}

		d.logger.Info("notifier: dispatcher: sent notification",
			"notification_id", cfg.ID, "event_type", event.Type)
	}
}

// matchesFilters returns true if all configured filters on cfg match the event.
// Each filter is optional (nil pointer = not set = always matches).
func matchesFilters(cfg *model.Notification, event NotificationEvent) bool {
	if cfg.FilterRuleID != nil {
		matched := false
		for _, id := range event.RuleIDs {
			if id == *cfg.FilterRuleID {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	if cfg.FilterModelName != nil {
		if event.ModelName != *cfg.FilterModelName {
			return false
		}
	}

	if cfg.FilterModelResult != nil {
		if !matchModelResult(*cfg.FilterModelResult, event.Results) {
			return false
		}
	}

	return true
}

// matchModelResult performs a shallow JSON key-value match: every key in
// filterJSON must exist in resultsJSON with the same value (compared after
// individual JSON re-marshal). Extra keys in resultsJSON are allowed.
func matchModelResult(filterJSON, resultsJSON json.RawMessage) bool {
	if len(filterJSON) == 0 {
		return true
	}
	if len(resultsJSON) == 0 {
		return false
	}

	var filter map[string]interface{}
	if err := json.Unmarshal(filterJSON, &filter); err != nil {
		return false
	}
	var results map[string]interface{}
	if err := json.Unmarshal(resultsJSON, &results); err != nil {
		return false
	}

	for k, fv := range filter {
		rv, ok := results[k]
		if !ok {
			return false
		}
		// Compare via JSON marshal to normalise numeric types, etc.
		fb, err1 := json.Marshal(fv)
		rb, err2 := json.Marshal(rv)
		if err1 != nil || err2 != nil {
			return false
		}
		if string(fb) != string(rb) {
			return false
		}
	}
	return true
}

// buildMessage renders a PushoverMessage from a notification config and event.
func buildMessage(cfg *model.Notification, event NotificationEvent) (PushoverMessage, error) {
	text, err := RenderMessage(event, cfg.MessageTemplate)
	if err != nil {
		return PushoverMessage{}, fmt.Errorf("notifier: build message: %w", err)
	}

	msg := PushoverMessage{
		Message:  text,
		Priority: cfg.PushoverPriority,
		HTML:     cfg.PushoverHTML,
	}
	if cfg.PushoverTitle != nil {
		msg.Title = *cfg.PushoverTitle
	}
	if cfg.PushoverSound != nil {
		msg.Sound = *cfg.PushoverSound
	}
	if cfg.PushoverDevice != nil {
		msg.Device = *cfg.PushoverDevice
	}
	if cfg.PushoverURL != nil {
		msg.URL = ExpandURL(*cfg.PushoverURL, event)
	}
	if cfg.PushoverURLTitle != nil {
		msg.URLTitle = *cfg.PushoverURLTitle
	}
	if cfg.PushoverTTL != nil {
		msg.TTL = *cfg.PushoverTTL
	}
	if cfg.PushoverRetry != nil {
		msg.Retry = *cfg.PushoverRetry
	}
	if cfg.PushoverExpire != nil {
		msg.Expire = *cfg.PushoverExpire
	}

	return msg, nil
}

// SendTestNotification retrieves a notification config by id, constructs a
// synthetic test event, and sends it immediately.
func (d *Dispatcher) SendTestNotification(ctx context.Context, id int64) error {
	cfg, err := d.store.GetNotification(ctx, id)
	if err != nil {
		return fmt.Errorf("notifier: send test notification: get config: %w", err)
	}

	event := NotificationEvent{
		Type: cfg.EventType,
		Tweet: &model.Tweet{
			ID:           "0000000000000000000",
			AuthorID:     "0",
			AuthorHandle: "test_user",
			Text:         "This is a test notification from iran-watcher.",
			CreatedAt:    time.Now(),
			FetchedAt:    time.Now(),
		},
		ModelName: "test-model",
		Results:   json.RawMessage(`{"label":"test","score":1.0}`),
		Timestamp: time.Now(),
	}

	msg, err := buildMessage(cfg, event)
	if err != nil {
		return fmt.Errorf("notifier: send test notification: %w", err)
	}

	return d.client.Send(ctx, msg)
}
