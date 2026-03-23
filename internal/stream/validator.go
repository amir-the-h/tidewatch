package stream

import (
	"fmt"

	"github.com/amir-the-h/iran-watcher/internal/model"
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
func (v *RuleValidator) Validate(node model.ExprNode, resolve SourceResolver) error {
	if err := validateStructure(node); err != nil {
		return err
	}
	if err := validateSourceRefs(node, resolve); err != nil {
		return err
	}
	if !hasAtom(node, resolve) {
		return fmt.Errorf("rule must contain at least one standalone atom operator (not modifier-only)")
	}
	compiled, err := v.compiler.Compile(node, resolve)
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

func validateStructure(node model.ExprNode) error {
	if len(node.AND) == 1 {
		return fmt.Errorf("AND requires at least 2 children")
	}
	if len(node.OR) == 1 {
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
	return nil
}

func validateSourceRefs(node model.ExprNode, resolve SourceResolver) error {
	if node.Src != nil {
		if _, err := resolve(*node.Src); err != nil {
			return fmt.Errorf("source %d not found or inactive: %w", *node.Src, err)
		}
	}
	for _, child := range node.AND {
		if err := validateSourceRefs(child, resolve); err != nil {
			return err
		}
	}
	for _, child := range node.OR {
		if err := validateSourceRefs(child, resolve); err != nil {
			return err
		}
	}
	if node.NOT != nil {
		if err := validateSourceRefs(*node.NOT, resolve); err != nil {
			return err
		}
	}
	return nil
}

// hasAtom returns true if the tree contains at least one atom-class source
// or an inline op that is not in the modifier set.
func hasAtom(node model.ExprNode, resolve SourceResolver) bool {
	if node.Src != nil {
		src, err := resolve(*node.Src)
		if err != nil {
			return false
		}
		return src.Class == model.SourceClassAtom
	}
	if node.Op != "" {
		return !modifierInlineOps[node.Op]
	}
	for _, child := range node.AND {
		if hasAtom(child, resolve) {
			return true
		}
	}
	for _, child := range node.OR {
		if hasAtom(child, resolve) {
			return true
		}
	}
	if node.NOT != nil {
		return hasAtom(*node.NOT, resolve)
	}
	return false
}
