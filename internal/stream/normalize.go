package stream

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// xStreamEvent mirrors the X API v2 Filtered Stream event JSON structure.
type xStreamEvent struct {
	Data struct {
		ID        string `json:"id"`
		Text      string `json:"text"`
		AuthorID  string `json:"author_id"`
		CreatedAt string `json:"created_at"`
	} `json:"data"`
	Includes struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"users"`
	} `json:"includes"`
	MatchingRules []struct {
		ID  string `json:"id"`
		Tag string `json:"tag"`
	} `json:"matching_rules"`
}

// Normalize parses a raw X API v2 stream event JSON payload and returns a StreamTweet.
// Sets Tweet.FetchedAt to time.Now(). Sets Tweet.RawJSON to the raw bytes.
// author_handle is looked up from includes.users by matching author_id.
// If no matching user is found, author_handle is set to the author_id.
func Normalize(data []byte) (*model.StreamTweet, error) {
	var evt xStreamEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return nil, fmt.Errorf("stream: normalize: unmarshal event: %w", err)
	}

	createdAt, err := time.Parse(time.RFC3339Nano, evt.Data.CreatedAt)
	if err != nil {
		// Fallback: try without sub-second precision.
		createdAt, err = time.Parse(time.RFC3339, evt.Data.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("stream: normalize: parse created_at %q: %w", evt.Data.CreatedAt, err)
		}
	}

	// Resolve author handle from includes.users.
	authorHandle := evt.Data.AuthorID
	for _, u := range evt.Includes.Users {
		if u.ID == evt.Data.AuthorID {
			authorHandle = u.Username
			break
		}
	}

	tweet := &model.Tweet{
		ID:           evt.Data.ID,
		AuthorID:     evt.Data.AuthorID,
		AuthorHandle: authorHandle,
		Text:         evt.Data.Text,
		RawJSON:      data,
		CreatedAt:    createdAt,
		FetchedAt:    time.Now(),
	}

	rules := make([]model.MatchingRule, 0, len(evt.MatchingRules))
	for _, r := range evt.MatchingRules {
		rules = append(rules, model.MatchingRule{
			ID:  r.ID,
			Tag: r.Tag,
		})
	}

	return &model.StreamTweet{
		Tweet:         tweet,
		MatchingRules: rules,
	}, nil
}
