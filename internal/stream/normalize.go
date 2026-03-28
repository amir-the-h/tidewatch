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

	tweet := &model.Tweet{
		ID:           evt.Data.ID,
		AuthorID:     evt.Data.AuthorID,
		AuthorHandle: authorHandle,
		Text:         text,
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
