package stream

import (
	"fmt"
	"strings"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// SourceResolver looks up a Source by ID.
type SourceResolver func(id int64) (*model.Source, error)

// RuleCompiler compiles an ExprNode tree into an X Filtered Stream rule string.
type RuleCompiler struct{}

// NewRuleCompiler returns a new RuleCompiler.
func NewRuleCompiler() *RuleCompiler { return &RuleCompiler{} }

// Compile walks node and returns the X rule syntax string.
func (c *RuleCompiler) Compile(node model.ExprNode, resolve SourceResolver) (string, error) {
	return c.compile(node, resolve)
}

func (c *RuleCompiler) compile(node model.ExprNode, resolve SourceResolver) (string, error) {
	switch {
	case node.Src != nil:
		src, err := resolve(*node.Src)
		if err != nil {
			return "", fmt.Errorf("compiler: resolve source %d: %w", *node.Src, err)
		}
		return src.Render(), nil

	case node.Op != "":
		return node.Op, nil

	case len(node.AND) > 0:
		parts := make([]string, 0, len(node.AND))
		for _, child := range node.AND {
			s, err := c.compile(child, resolve)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, " "), nil

	case len(node.OR) > 0:
		parts := make([]string, 0, len(node.OR))
		for _, child := range node.OR {
			s, err := c.compile(child, resolve)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil

	case node.NOT != nil:
		inner, err := c.compile(*node.NOT, resolve)
		if err != nil {
			return "", err
		}
		// OR already wraps itself in (...), so just prepend -.
		// AND is space-joined without parens, so wrap it.
		if len(node.NOT.AND) > 0 {
			return "-(" + inner + ")", nil
		}
		return "-" + inner, nil

	default:
		return "", fmt.Errorf("compiler: empty node")
	}
}
