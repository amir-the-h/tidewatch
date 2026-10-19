package search

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// xSearchResponse mirrors the X API v2 search response JSON structure.
type xSearchResponse struct {
	Data     []xSearchTweet `json:"data"`
	Includes struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"users"`
		Tweets []struct {
			ID        string `json:"id"`
			Text      string `json:"text"`
			NoteTweet *struct {
				Text string `json:"text"`
			} `json:"note_tweet"`
		} `json:"tweets"`
	} `json:"includes"`
	Meta struct {
		NewestID    string `json:"newest_id"`
		OldestID    string `json:"oldest_id"`
		ResultCount int    `json:"result_count"`
		NextToken   string `json:"next_token"`
	} `json:"meta"`
}

// xSearchTweet is a single tweet data object from the search response.
type xSearchTweet struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	AuthorID  string `json:"author_id"`
	CreatedAt string `json:"created_at"`
	NoteTweet *struct {
		Text string `json:"text"`
	} `json:"note_tweet"`
	ReferencedTweets []struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"referenced_tweets"`
}

// NormalizedPage holds the tweets parsed from a single search response page
// along with the pagination token for the next page (empty if no more pages).
type NormalizedPage struct {
	Tweets    []*model.StreamTweet
	NextToken string
	NewestID  string // meta.newest_id from the X API response; empty when the page has no results
}

// NormalizeSearchResponse parses an X API v2 search response body and
// returns one StreamTweet per result, all attributed to the given rule.
func NormalizeSearchResponse(body []byte, rule *model.Rule) (*NormalizedPage, error) {
	var resp xSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("search: normalize: unmarshal response: %w", err)
	}

	// Build a lookup map for users by ID.
	userByID := make(map[string]string, len(resp.Includes.Users))
	for _, u := range resp.Includes.Users {
		userByID[u.ID] = u.Username
	}

	// Build a lookup map for included tweets by ID (for retweet full text).
	// Prefer note_tweet.text over text for long-form tweets.
	tweetTextByID := make(map[string]string, len(resp.Includes.Tweets))
	for _, t := range resp.Includes.Tweets {
		if t.NoteTweet != nil && t.NoteTweet.Text != "" {
			tweetTextByID[t.ID] = t.NoteTweet.Text
		} else {
			tweetTextByID[t.ID] = t.Text
		}
	}

	tweets := make([]*model.StreamTweet, 0, len(resp.Data))
	for _, d := range resp.Data {
		var createdAt time.Time
		if d.CreatedAt != "" {
			var parseErr error
			createdAt, parseErr = time.Parse(time.RFC3339Nano, d.CreatedAt)
			if parseErr != nil {
				createdAt, parseErr = time.Parse(time.RFC3339, d.CreatedAt)
				if parseErr != nil {
					createdAt = time.Now()
				}
			}
		} else {
			createdAt = time.Now()
		}

		// Resolve author handle; fall back to author_id if not found.
		authorHandle := d.AuthorID
		if handle, ok := userByID[d.AuthorID]; ok {
			authorHandle = handle
		}

		// Prefer note_tweet.text for long-form tweets (> 280 chars); fall back to text.
		text := d.Text
		if d.NoteTweet != nil && d.NoteTweet.Text != "" {
			text = d.NoteTweet.Text
		}

		// For retweets, substitute the full text from includes.tweets.
		for _, ref := range d.ReferencedTweets {
			if ref.Type == "retweeted" {
				if fullText, ok := tweetTextByID[ref.ID]; ok && fullText != "" {
					text = fullText
				}
				break
			}
		}

		// Marshal just the individual tweet data object as RawJSON.
		rawJSON, err := json.Marshal(d)
		if err != nil {
			return nil, fmt.Errorf("search: normalize: marshal raw tweet %s: %w", d.ID, err)
		}

		tweet := &model.Tweet{
			ID:           d.ID,
			AuthorID:     d.AuthorID,
			AuthorHandle: authorHandle,
			Text:         text,
			RawJSON:      rawJSON,
			CreatedAt:    createdAt,
			FetchedAt:    time.Now(),
		}

		st := &model.StreamTweet{
			Tweet: tweet,
			MatchingRules: []model.MatchingRule{
				{ID: rule.XRuleID, Tag: rule.Name},
			},
		}
		tweets = append(tweets, st)
	}

	return &NormalizedPage{
		Tweets:    tweets,
		NextToken: resp.Meta.NextToken,
		NewestID:  resp.Meta.NewestID,
	}, nil
}
