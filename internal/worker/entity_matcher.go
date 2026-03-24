package worker

import (
	"context"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// matchEntities loads all active tracked entities and checks whether the tweet
// text or any NER-extracted entity matches. A track record is written for each
// match (duplicate (entity_id, tweet_id) pairs are silently ignored by the store).
func (w *worker) matchEntities(ctx context.Context, tweet *model.Tweet, nerEntities []model.NEREntity) {
	entities, err := w.store.ListActiveTrackedEntities(ctx)
	if err != nil {
		if w.logger != nil {
			w.logger.Warn("worker: match entities: list active", "error", err)
		}
		return
	}
	if len(entities) == 0 {
		return
	}

	tweetTextLower := strings.ToLower(tweet.Text)

	for _, entity := range entities {
		// Build the full candidate set: name + aliases.
		candidates := make([]string, 0, 1+len(entity.Aliases))
		candidates = append(candidates, entity.Name)
		candidates = append(candidates, entity.Aliases...)

		matchSource, context := matchCandidate(tweetTextLower, candidates, nerEntities)
		if matchSource == "" {
			continue
		}

		track := &model.EntityTrack{
			EntityID:    entity.ID,
			TweetID:     tweet.ID,
			MatchSource: matchSource,
			Context:     context,
		}
		if saveErr := w.store.SaveEntityTrack(ctx, track); saveErr != nil {
			if w.logger != nil {
				w.logger.Warn("worker: match entities: save track",
					"entity_id", entity.ID,
					"tweet_id", tweet.ID,
					"error", saveErr,
				)
			}
		}
	}
}

// matchCandidate returns the match source ("ner" preferred over "text") and a
// context snippet if any candidate is found in the tweet text or NER entities.
func matchCandidate(tweetTextLower string, candidates []string, nerEntities []model.NEREntity) (matchSource, context string) {
	for _, c := range candidates {
		cLower := strings.ToLower(c)

		// Prefer NER match since it is more precise.
		for _, ne := range nerEntities {
			if strings.ToLower(ne.Word) == cLower {
				return "ner", ne.Word
			}
		}

		// Fall back to plain text match.
		if strings.Contains(tweetTextLower, cLower) {
			return "text", c
		}
	}
	return "", ""
}
