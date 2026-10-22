package stream

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// xStreamEvent mirrors the X API v2 Filtered Stream event JSON structure.
type xStreamEvent struct {
	Data struct {
		ID        string `json:"id"`
		Text      string `json:"text"`
		AuthorID  string `json:"author_id"`
		CreatedAt string `json:"created_at"`
		NoteTweet *struct {
			Text string `json:"text"`
		} `json:"note_tweet"`
		ReferencedTweets []struct {
			ID   string `json:"id"`
			Type string `json:"type"` // "retweeted", "quoted", "replied_to"
		} `json:"referenced_tweets"`
	} `json:"data"`
	Includes struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"users"`
		Tweets []struct {
			ID        string `json:"id"`
			Text      string `json:"text"`
			NoteTweet *struct {
				Text string `json:"text"`
			} `json:"note_tweet"`
		} `json:"tweets"`
	} `json:"includes"`
	MatchingRules []struct {
		ID  string `json:"id"`
		Tag string `json:"tag"`
	} `json:"matching_rules"`
}

// Event is a normalized X stream event: the post signal plus the X rules the
// stream says it matched (resolved to local rule IDs by the caller).
type Event struct {
	Signal        *model.Signal
	MatchingRules []MatchingRule
}

// Normalize parses a raw X API v2 stream event JSON payload into an Event.
// The signal's author (handle) is looked up from includes.users by matching
// author_id; if no matching user is found, the author_id is used.
func Normalize(data []byte) (*Event, error) {
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

	// Prefer note_tweet.text for long-form tweets (> 280 chars); fall back to text.
	text := evt.Data.Text
	if evt.Data.NoteTweet != nil && evt.Data.NoteTweet.Text != "" {
		text = evt.Data.NoteTweet.Text
	}

	// For retweets, the main tweet text is truncated at 280 chars.
	// Substitute the full text from includes.tweets when available.
	for _, ref := range evt.Data.ReferencedTweets {
		if ref.Type == "retweeted" {
			for _, t := range evt.Includes.Tweets {
				if t.ID == ref.ID {
					if t.NoteTweet != nil && t.NoteTweet.Text != "" {
						text = t.NoteTweet.Text
					} else if t.Text != "" {
						text = t.Text
					}
				}
			}
			break
		}
	}

	var referenced map[string]string
	for _, ref := range evt.Data.ReferencedTweets {
		if referenced == nil {
			referenced = make(map[string]string, len(evt.Data.ReferencedTweets))
		}
		referenced[ref.Type] = ref.ID
	}

	sig := NewXSignal(XPost{
		ID:         evt.Data.ID,
		AuthorID:   evt.Data.AuthorID,
		Handle:     authorHandle,
		Text:       text,
		CreatedAt:  createdAt,
		RawJSON:    data,
		Referenced: referenced,
	})

	rules := make([]MatchingRule, 0, len(evt.MatchingRules))
	for _, r := range evt.MatchingRules {
		rules = append(rules, MatchingRule{
			ID:  r.ID,
			Tag: r.Tag,
		})
	}

	return &Event{
		Signal:        sig,
		MatchingRules: rules,
	}, nil
}
