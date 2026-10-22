package source

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// ValidationError is a source definition the API should reject with 400.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalid(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// Validate checks a source against its connector: the type is registered, the
// config is a JSON object with every required field set and every select
// field one of its options, the connector's own Validate passes, the poll
// interval respects the connector's minimum, and the tier is 1–4.
func (r *Registry) Validate(src *model.Source) error {
	if src.Name == "" {
		return invalid("name is required")
	}
	conn, ok := r.Get(src.Type)
	if !ok {
		return invalid("unknown source type %q", src.Type)
	}
	d := conn.Descriptor()

	cfg := src.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}
	var m map[string]any
	if err := json.Unmarshal(cfg, &m); err != nil || m == nil {
		return invalid("config must be a JSON object")
	}
	for _, f := range d.Fields {
		v, present := m[f.Key]
		if f.Required && (!present || v == nil || v == "") {
			return invalid("config.%s is required", f.Key)
		}
		if f.Type == FieldSelect && present && v != nil && v != "" {
			s, isString := v.(string)
			if !isString || !slices.Contains(f.Options, s) {
				return invalid("config.%s must be one of %v", f.Key, f.Options)
			}
		}
	}
	if err := conn.Validate(cfg); err != nil {
		return invalid("invalid config: %v", err)
	}

	if src.PollIntervalS != nil {
		if d.Mode != ModePoll {
			return invalid("poll_interval_s only applies to poll sources")
		}
		iv := time.Duration(*src.PollIntervalS) * time.Second
		if iv <= 0 {
			return invalid("poll_interval_s must be positive")
		}
		if d.MinInterval > 0 && iv < d.MinInterval {
			return invalid("poll_interval_s must be at least %d for %s", int(d.MinInterval/time.Second), d.Type)
		}
	}
	if src.Tier != nil && (*src.Tier < 1 || *src.Tier > 4) {
		return invalid("tier must be between 1 and 4")
	}
	return nil
}

// CheckInstances returns a ValidationError when activating src would exceed
// its connector's MaxInstances, given every stored source.
func (r *Registry) CheckInstances(src *model.Source, all []*model.Source) error {
	conn, ok := r.Get(src.Type)
	if !ok || !src.Active {
		return nil
	}
	maxN := conn.Descriptor().MaxInstances
	if maxN == 0 {
		return nil
	}
	n := 0
	for _, o := range all {
		if o.Active && o.Type == src.Type && o.ID != src.ID {
			n++
		}
	}
	if n >= maxN {
		return invalid("at most %d active %s source(s) allowed", maxN, src.Type)
	}
	return nil
}

// interval returns the poll interval of src: its own, else the connector
// default, else 10 minutes.
func interval(src *model.Source, d Descriptor) time.Duration {
	if src.PollIntervalS != nil && *src.PollIntervalS > 0 {
		return time.Duration(*src.PollIntervalS) * time.Second
	}
	if d.DefaultInterval > 0 {
		return d.DefaultInterval
	}
	return 10 * time.Minute
}
