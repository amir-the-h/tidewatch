package source

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/amir-the-h/tidewatch/internal/hub"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/notifier"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/internal/worker"
)

// EmitOption adjusts how one emitted signal is ingested.
type EmitOption func(*emitOptions)

type emitOptions struct {
	xRuleIDs  []int64
	hasXRules bool
}

// WithXRuleMatches attaches the X stream rules a tweet matched: they are
// stored in x_rule_matches and set as the x_rule_ids attr.
func WithXRuleMatches(ruleIDs []int64) EmitOption {
	return func(o *emitOptions) {
		o.xRuleIDs = ruleIDs
		o.hasXRules = true
	}
}

// Ingester is the one ingest path every connector goes through. Producer may
// be nil (nothing is dispatched); Cache, Broadcaster, Emitter and Logger may
// be nil.
type Ingester struct {
	Store       store.Store
	Producer    queue.Producer
	Cache       *worker.ActionCache
	Broadcaster hub.Broadcaster
	Emitter     notifier.EventEmitter
	Logger      *slog.Logger
}

// Ingest stores sig and dispatches it when it is new:
//
//  1. insert, or find the existing row with the same (ext_ns, external_id);
//  2. record that src saw it (signal_sources) and any attached X rule matches;
//  3. if new: notifier event, WebSocket new_signal, and new_signal pipeline
//     tasks carrying the source ID and type.
//
// A signal that already existed only gains the step 2 rows, so a tweet found
// by search after the stream delivered it is not processed twice.
//
// src may be nil for ingest that has no source row yet (the X shim until
// phase 1 step 3); sourceType is then reported on its own.
func (in *Ingester) Ingest(ctx context.Context, src *model.Source, sourceType string, sig *model.Signal, opts ...EmitOption) (bool, error) {
	var o emitOptions
	for _, opt := range opts {
		opt(&o)
	}
	if src != nil {
		sourceType = src.Type
	}
	if sig.Attrs == nil {
		sig.Attrs = map[string]any{}
	}
	if o.hasXRules {
		if o.xRuleIDs == nil {
			o.xRuleIDs = []int64{}
		}
		sig.Attrs["x_rule_ids"] = o.xRuleIDs
	}
	if sig.FetchedAt.IsZero() {
		sig.FetchedAt = time.Now()
	}

	isNew, err := in.Store.SaveSignal(ctx, sig)
	if err != nil {
		return false, fmt.Errorf("source: save signal %s/%s: %w", sig.ExtNS, sig.ExternalID, err)
	}

	if src != nil {
		if err := in.Store.AddSignalSource(ctx, sig.ID, src.ID); err != nil {
			in.warn("source: save signal source failed", "signal_id", sig.ID, "source_id", src.ID, "error", err)
		}
	}
	for _, ruleID := range o.xRuleIDs {
		if err := in.Store.SaveXRuleMatch(ctx, &model.XRuleMatch{SignalID: sig.ID, RuleID: ruleID}); err != nil {
			in.warn("source: save x rule match failed", "signal_id", sig.ID, "rule_id", ruleID, "error", err)
		}
	}

	if !isNew {
		return false, nil
	}

	if in.Emitter != nil {
		in.Emitter.Emit(notifier.NotificationEvent{
			Type:      notifier.EventSignalIngested,
			Signal:    sig,
			RuleIDs:   o.xRuleIDs,
			Timestamp: time.Now(),
		})
	}
	if in.Broadcaster != nil {
		in.Broadcaster.Broadcast(hub.Event{Type: "new_signal", Payload: sig})
	}

	if in.Producer != nil {
		ev := queue.SignalEvent{SignalID: sig.ID, Kind: string(sig.Kind), SourceType: sourceType}
		if src != nil {
			ev.SourceID = src.ID
		}
		if err := worker.PushInitialPipelineTasks(ctx, in.Store, in.Producer, in.Cache, "new_signal", ev, in.Logger, 0); err != nil {
			in.warn("source: pipeline dispatch failed", "signal_id", sig.ID, "error", err)
		}
	}
	return true, nil
}

func (in *Ingester) warn(msg string, args ...any) {
	if in.Logger != nil {
		in.Logger.Warn(msg, args...)
	}
}
