package x

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// SourceResolver looks up a Source by ID.
type SourceResolver func(id int64) (*model.XTerm, error)

// RuleResolver looks up a local Rule by ID.
type RuleResolver func(id int64) (*model.XRule, error)

// RuleCompiler compiles an ExprNode tree into an X Filtered Stream rule string.
type RuleCompiler struct{}

// NewRuleCompiler returns a new RuleCompiler.
func NewRuleCompiler() *RuleCompiler { return &RuleCompiler{} }

// Compile walks node and returns the X rule syntax string.
func (c *RuleCompiler) Compile(node model.ExprNode, resolve SourceResolver) (string, error) {
	return c.CompileWithRules(node, resolve, nil)
}

// CompileWithRules walks node and returns the X rule syntax string.
// resolveRule may be nil if no rule references are expected.
func (c *RuleCompiler) CompileWithRules(node model.ExprNode, resolve SourceResolver, resolveRule RuleResolver) (string, error) {
	return c.compileNode(node, resolve, resolveRule, make(map[int64]bool))
}

func (c *RuleCompiler) compileNode(node model.ExprNode, resolve SourceResolver, resolveRule RuleResolver, visited map[int64]bool) (string, error) {
	switch {
	case node.Src != nil:
		src, err := resolve(*node.Src)
		if err != nil {
			return "", fmt.Errorf("compiler: resolve source %d: %w", *node.Src, err)
		}
		return src.Render(), nil

	case len(node.Srcs) > 0:
		parts := make([]string, 0, len(node.Srcs))
		for _, id := range node.Srcs {
			src, err := resolve(id)
			if err != nil {
				return "", fmt.Errorf("compiler: resolve source %d: %w", id, err)
			}
			parts = append(parts, src.Render())
		}
		if len(parts) == 1 {
			return parts[0], nil
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil

	case node.Op != "":
		return node.Op, nil

	case len(node.AND) > 0:
		parts := make([]string, 0, len(node.AND))
		for _, child := range node.AND {
			s, err := c.compileNode(child, resolve, resolveRule, visited)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, " "), nil

	case len(node.OR) > 0:
		// OR([srcs[...]]) — unwrap: srcs already compiles to (s1 OR s2 OR ...)
		if len(node.OR) == 1 && len(node.OR[0].Srcs) > 0 {
			return c.compileNode(node.OR[0], resolve, resolveRule, visited)
		}
		parts := make([]string, 0, len(node.OR))
		for _, child := range node.OR {
			s, err := c.compileNode(child, resolve, resolveRule, visited)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil

	case node.NOT != nil:
		inner, err := c.compileNode(*node.NOT, resolve, resolveRule, visited)
		if err != nil {
			return "", err
		}
		// OR already wraps itself in (...), so just prepend -.
		// AND is space-joined without parens, so wrap it.
		if len(node.NOT.AND) > 0 {
			return "-(" + inner + ")", nil
		}
		return "-" + inner, nil

	case node.Rule != nil:
		if resolveRule == nil {
			return "", fmt.Errorf("compiler: rule references not supported")
		}
		id := *node.Rule
		if visited[id] {
			return "", fmt.Errorf("compiler: circular rule reference detected (rule %d)", id)
		}
		r, err := resolveRule(id)
		if err != nil {
			return "", fmt.Errorf("compiler: resolve rule %d: %w", id, err)
		}
		var inner model.ExprNode
		if err := json.Unmarshal(r.Expression, &inner); err != nil {
			return "", fmt.Errorf("compiler: parse rule %d expression: %w", id, err)
		}
		visited[id] = true
		result, err := c.compileNode(inner, resolve, resolveRule, visited)
		delete(visited, id)
		return result, err

	default:
		return "", fmt.Errorf("compiler: empty node")
	}
}
