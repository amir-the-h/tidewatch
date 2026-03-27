package worker

import (
	"github.com/amir-the-h/iran-watcher/internal/action"
	"github.com/amir-the-h/iran-watcher/internal/model"
)

// ActionTask is a unit of work: execute one action for one tweet.
// The action is fully self-contained — it handles categorization, entity
// matching, and dead-letter writes internally.
type ActionTask struct {
	Tweet  *model.StreamTweet
	Action action.Action
}
