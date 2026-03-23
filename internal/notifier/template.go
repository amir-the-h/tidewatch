package notifier

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

var defaultTemplates = map[string]string{
	EventTweetMatched:         `New tweet from @{{.Tweet.AuthorHandle}}: {{.Tweet.Text}}`,
	EventTweetCategorized:     `Tweet by @{{.Tweet.AuthorHandle}} categorized by {{.ModelName}}: {{.Results}}`,
	EventCategorizationFailed: `Failed to categorize tweet {{.Tweet.ID}} with {{.ModelName}}: {{.Error}}`,
	EventStreamConnected:      `Stream connected`,
	EventStreamDisconnected:   `Stream disconnected`,
	EventStreamError:          `Stream error: {{.Error}}`,
}

func RenderMessage(event NotificationEvent, customTemplate *string) (string, error) {
	tmplStr := defaultTemplates[event.Type]
	if customTemplate != nil && *customTemplate != "" {
		tmplStr = *customTemplate
	}
	if tmplStr == "" {
		return fmt.Sprintf("Event: %s", event.Type), nil
	}

	t, err := template.New("msg").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("notifier: parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, event); err != nil {
		return "", fmt.Errorf("notifier: execute template: %w", err)
	}
	return buf.String(), nil
}

func ValidateTemplate(tmpl string) error {
	_, err := template.New("validate").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("notifier: invalid template: %w", err)
	}
	return nil
}

func ExpandURL(urlTemplate string, event NotificationEvent) string {
	tweetID := ""
	authorHandle := ""
	if event.Tweet != nil {
		tweetID = event.Tweet.ID
		authorHandle = event.Tweet.AuthorHandle
	}
	r := strings.NewReplacer(
		"{tweet_id}", tweetID,
		"{author_handle}", authorHandle,
	)
	return r.Replace(urlTemplate)
}
