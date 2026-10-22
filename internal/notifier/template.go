package notifier

import (
	"bytes"
	"fmt"
	"text/template"
)

var defaultTemplates = map[string]string{
	EventSignalIngested: `New signal from {{.Signal.Author}}: {{.Signal.Text}}`,
	EventDigestDone:     `Digest ready ({{.DigestJob.SignalCount}} signals): {{.DigestJob.Result}}`,
	EventDigestFailed:   `Digest failed: {{.Error}}`,
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
	t, err := template.New("url").Parse(urlTemplate)
	if err != nil {
		return urlTemplate
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, event); err != nil {
		return urlTemplate
	}
	return buf.String()
}
