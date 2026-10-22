package x

import (
	"encoding/json"
	"fmt"

	"github.com/amir-the-h/tidewatch/internal/model"
)

// modifierInlineOps is the set of inline op values that are
// conjunction-required (modifier) operators in the X API.
var modifierInlineOps = map[string]bool{
	"is:retweet": true, "is:reply": true, "is:quote": true, "is:verified": true,
	"has:hashtags": true, "has:cashtags": true, "has:links": true,
	"has:mentions": true, "has:media": true, "has:images": true,
	"has:video_link": true, "has:geo": true,
}

const (
	maxLengthBasic      = 1024
	maxLengthEnterprise = 2048
)

// RuleValidator validates an ExprNode tree before it is saved and pushed to X.
type RuleValidator struct {
	tier     string
	compiler *RuleCompiler
}

// NewRuleValidator returns a validator for the given tier ("basic" or "enterprise").
func NewRuleValidator(tier string) *RuleValidator {
	return &RuleValidator{tier: tier, compiler: NewRuleCompiler()}
}

// Validate checks the expression tree and returns an error if any rule is violated.
// resolveRule may be nil if the expression contains no rule references.
func (v *RuleValidator) Validate(node model.ExprNode, resolve SourceResolver, resolveRule RuleResolver) error {
	if err := validateStructure(node); err != nil {
		return err
	}
	if err := validateRefs(node, resolve, resolveRule); err != nil {
		return err
	}
	if !hasAtom(node, resolve, resolveRule, make(map[int64]bool)) {
		return fmt.Errorf("rule must contain at least one standalone atom operator (not modifier-only)")
	}
	compiled, err := v.compiler.CompileWithRules(node, resolve, resolveRule)
	if err != nil {
		return fmt.Errorf("validator: compile: %w", err)
	}
	limit := maxLengthBasic
	if v.tier == "enterprise" {
		limit = maxLengthEnterprise
	}
	if len(compiled) > limit {
		return fmt.Errorf("compiled rule length %d exceeds tier limit %d", len(compiled), limit)
	}
	return nil
}

// ValidateLocalRule checks the expression tree for a local rule.
// It skips hasAtom and length checks since local rules are building blocks.
func (v *RuleValidator) ValidateLocalRule(node model.ExprNode, resolve SourceResolver, resolveRule RuleResolver) error {
	if err := validateStructure(node); err != nil {
		return err
	}
	if err := validateRefs(node, resolve, resolveRule); err != nil {
		return err
	}
	return nil
}

func validateStructure(node model.ExprNode) error {
	if len(node.AND) == 1 {
		return fmt.Errorf("AND requires at least 2 children")
	}
	// A single srcs child is allowed inside OR — srcs already encodes OR semantics
	// and the compiler flattens it, so OR([srcs[...]]) is semantically valid.
	if len(node.OR) == 1 && len(node.OR[0].Srcs) == 0 {
		return fmt.Errorf("OR requires at least 2 children")
	}
	for _, child := range node.AND {
		if err := validateStructure(child); err != nil {
			return err
		}
	}
	for _, child := range node.OR {
		if err := validateStructure(child); err != nil {
			return err
		}
	}
	if node.NOT != nil {
		if err := validateStructure(*node.NOT); err != nil {
			return err
		}
	}
	// Rule nodes have no structural constraint.
	return nil
}

func validateRefs(node model.ExprNode, resolve SourceResolver, resolveRule RuleResolver) error {
	if node.Src != nil {
		if _, err := resolve(*node.Src); err != nil {
			return fmt.Errorf("source %d not found or inactive: %w", *node.Src, err)
		}
	}
	for _, id := range node.Srcs {
		if _, err := resolve(id); err != nil {
			return fmt.Errorf("source %d not found or inactive: %w", id, err)
		}
	}
	if node.Rule != nil {
		if resolveRule == nil {
			return fmt.Errorf("rule references not supported")
		}
		if _, err := resolveRule(*node.Rule); err != nil {
			return fmt.Errorf("rule %d not found or inactive: %w", *node.Rule, err)
		}
	}
	for _, child := range node.AND {
		if err := validateRefs(child, resolve, resolveRule); err != nil {
			return err
		}
	}
	for _, child := range node.OR {
		if err := validateRefs(child, resolve, resolveRule); err != nil {
			return err
		}
	}
	if node.NOT != nil {
		if err := validateRefs(*node.NOT, resolve, resolveRule); err != nil {
			return err
		}
	}
	return nil
}

// hasAtom returns true if the tree contains at least one atom-class source
// or an inline op that is not in the modifier set.
func hasAtom(node model.ExprNode, resolve SourceResolver, resolveRule RuleResolver, visited map[int64]bool) bool {
	if node.Src != nil {
		src, err := resolve(*node.Src)
		if err != nil {
			return false
		}
		return src.Class == model.XTermClassAtom
	}
	for _, id := range node.Srcs {
		src, err := resolve(id)
		if err != nil {
			continue
		}
		if src.Class == model.XTermClassAtom {
			return true
		}
	}
	if node.Op != "" {
		return !modifierInlineOps[node.Op]
	}
	if node.Rule != nil && resolveRule != nil {
		id := *node.Rule
		if visited[id] {
			return false
		}
		r, err := resolveRule(id)
		if err != nil {
			return false
		}
		var inner model.ExprNode
		if err := json.Unmarshal(r.Expression, &inner); err != nil {
			return false
		}
		visited[id] = true
		result := hasAtom(inner, resolve, resolveRule, visited)
		delete(visited, id)
		return result
	}
	for _, child := range node.AND {
		if hasAtom(child, resolve, resolveRule, visited) {
			return true
		}
	}
	for _, child := range node.OR {
		if hasAtom(child, resolve, resolveRule, visited) {
			return true
		}
	}
	if node.NOT != nil {
		return hasAtom(*node.NOT, resolve, resolveRule, visited)
	}
	return false
}
