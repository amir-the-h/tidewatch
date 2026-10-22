// Package source is the connector framework: a connector type is code that
// knows how to talk to one upstream, a source is a configured instance of it
// (model.Source). The Manager runs active sources, the Ingester persists and
// dispatches what they emit.
package source

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// Mode is how the manager drives a connector.
type Mode string

const (
	ModeStream Mode = "stream" // long-lived connection; Run blocks
	ModePoll   Mode = "poll"   // the manager calls Poll on an interval
)

// FieldType is the input type of a config field, used to render forms.
type FieldType string

const (
	FieldString     FieldType = "string"
	FieldText       FieldType = "text" // multi-line string
	FieldInt        FieldType = "int"
	FieldBool       FieldType = "bool"
	FieldURL        FieldType = "url"
	FieldSelect     FieldType = "select" // one of Options
	FieldStringList FieldType = "string_list"
)

// Field describes one key of a connector's config JSON.
type Field struct {
	Key      string    `json:"key"`
	Label    string    `json:"label"`
	Type     FieldType `json:"type"`
	Required bool      `json:"required"`
	// Secret fields are masked by the API on read (see MaskConfig).
	Secret  bool     `json:"secret"`
	Default any      `json:"default,omitempty"`
	Help    string   `json:"help,omitempty"`
	Options []string `json:"options,omitempty"`
}

// Descriptor tells the API and UI how to configure a connector type.
type Descriptor struct {
	Type   string // "rss"
	Label  string // "RSS / Atom feed"
	Mode   Mode
	ExtNS  string // dedup namespace of the signals it emits
	Kinds  []model.SignalKind
	Fields []Field
	// MaxInstances caps how many sources of this type can be active at once;
	// 0 = unlimited (x_stream = 1: one stream per X app).
	MaxInstances    int
	DefaultInterval time.Duration // poll only
	MinInterval     time.Duration // poll only; floor enforced on create/update
	Backfill        bool          // supports source runs of kind backfill
}

// MarshalJSON renders durations as whole seconds.
func (d Descriptor) MarshalJSON() ([]byte, error) {
	kinds := d.Kinds
	if kinds == nil {
		kinds = []model.SignalKind{}
	}
	fields := d.Fields
	if fields == nil {
		fields = []Field{}
	}
	return json.Marshal(struct {
		Type             string             `json:"type"`
		Label            string             `json:"label"`
		Mode             Mode               `json:"mode"`
		ExtNS            string             `json:"ext_ns"`
		Kinds            []model.SignalKind `json:"kinds"`
		Fields           []Field            `json:"fields"`
		MaxInstances     int                `json:"max_instances"`
		DefaultIntervalS int                `json:"default_interval_s,omitempty"`
		MinIntervalS     int                `json:"min_interval_s,omitempty"`
		Backfill         bool               `json:"backfill"`
		Fetch            bool               `json:"fetch"` // manual fetch: poll connectors only
	}{
		d.Type, d.Label, d.Mode, d.ExtNS, kinds, fields, d.MaxInstances,
		int(d.DefaultInterval / time.Second), int(d.MinInterval / time.Second), d.Backfill, d.Mode == ModePoll,
	})
}

// Connector is implemented by every connector type, together with exactly one
// of StreamConnector or PollConnector.
type Connector interface {
	Descriptor() Descriptor
	// Validate checks a source config beyond the generic required-field check.
	Validate(cfg json.RawMessage) error
}

// StreamConnector holds a long-lived connection. Run blocks until ctx is
// cancelled or the connection fails; the manager restarts it with backoff.
type StreamConnector interface {
	Connector
	Run(ctx context.Context, src *model.Source, rt Runtime) error
}

// PollConnector fetches once per call. It reads its cursor from rt, emits
// signals and sets the new cursor; the manager persists the cursor only when
// Poll succeeds.
type PollConnector interface {
	Connector
	Poll(ctx context.Context, src *model.Source, rt Runtime) error
}

// Backfiller is optionally implemented by connectors whose descriptor sets
// Backfill: an on-demand historical fetch with connector-defined params.
type Backfiller interface {
	Backfill(ctx context.Context, src *model.Source, params json.RawMessage, rt Runtime) error
}

// Runtime is what a connector gets from the manager.
type Runtime interface {
	// Emit persists a signal and, when it is new, dispatches it to pipelines.
	Emit(ctx context.Context, s *model.Signal, opts ...EmitOption) (isNew bool, err error)
	Cursor() json.RawMessage
	SetCursor(json.RawMessage)
	HTTPClient() *http.Client
	Logger() *slog.Logger
}
