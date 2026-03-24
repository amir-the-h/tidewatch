package stream_test

import (
	"strings"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/stream"
)

func TestValidateOK(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{AND: []model.ExprNode{
		{Src: ptr[int64](1)}, // atom: from:BBCPersian
		{Src: ptr[int64](4)}, // modifier: lang:fa
	}}
	if err := v.Validate(node, mockResolver(testSources), nil); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateModifierOnly(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{Src: ptr[int64](4)} // lang:fa alone
	if err := v.Validate(node, mockResolver(testSources), nil); err == nil {
		t.Error("expected error for modifier-only rule")
	}
}

func TestValidateInlineModifierOnly(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{Op: "is:retweet"}
	if err := v.Validate(node, mockResolver(testSources), nil); err == nil {
		t.Error("expected error for inline modifier-only rule")
	}
}

func TestValidateInlineAtomOnly(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{Op: "iran"}
	if err := v.Validate(node, mockResolver(testSources), nil); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateTooLong(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	src := &model.Source{ID: 99, Operator: "from:", Value: strings.Repeat("a", 1020), Class: model.SourceClassAtom}
	sources := map[int64]*model.Source{99: src}
	node := model.ExprNode{Src: ptr[int64](99)}
	if err := v.Validate(node, mockResolver(sources), nil); err == nil {
		t.Error("expected error for too-long rule")
	}
}

func TestValidateANDRequiresTwoChildren(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{AND: []model.ExprNode{{Src: ptr[int64](1)}}}
	if err := v.Validate(node, mockResolver(testSources), nil); err == nil {
		t.Error("expected error for AND with one child")
	}
}

func TestValidateORRequiresTwoChildren(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{OR: []model.ExprNode{{Src: ptr[int64](1)}}}
	if err := v.Validate(node, mockResolver(testSources), nil); err == nil {
		t.Error("expected error for OR with one child")
	}
}

func TestValidateMissingSource(t *testing.T) {
	v := stream.NewRuleValidator("basic")
	node := model.ExprNode{Src: ptr[int64](999)}
	if err := v.Validate(node, mockResolver(testSources), nil); err == nil {
		t.Error("expected error for missing source")
	}
}
