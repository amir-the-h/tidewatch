package stream_test

import (
	"errors"
	"testing"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/stream"
)

func ptr[T any](v T) *T { return &v }

func mockResolver(sources map[int64]*model.XTerm) stream.SourceResolver {
	return func(id int64) (*model.XTerm, error) {
		s, ok := sources[id]
		if !ok {
			return nil, errors.New("not found")
		}
		return s, nil
	}
}

var testSources = map[int64]*model.XTerm{
	1: {ID: 1, Operator: "from:", Value: "BBCPersian", Class: model.XTermClassAtom},
	2: {ID: 2, Operator: "from:", Value: "IranIntl", Class: model.XTermClassAtom},
	3: {ID: 3, Operator: "", Value: "ایران", Class: model.XTermClassAtom},
	4: {ID: 4, Operator: "lang:", Value: "fa", Class: model.XTermClassModifier},
	5: {ID: 5, Operator: "", Value: `"breaking news"`, Class: model.XTermClassAtom},
}

func TestCompileSingleSrc(t *testing.T) {
	c := stream.NewRuleCompiler()
	result, err := c.Compile(model.ExprNode{Src: ptr[int64](1)}, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from:BBCPersian" {
		t.Errorf("got %q, want %q", result, "from:BBCPersian")
	}
}

func TestCompileInlineOp(t *testing.T) {
	c := stream.NewRuleCompiler()
	result, err := c.Compile(model.ExprNode{Op: "is:retweet"}, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "is:retweet" {
		t.Errorf("got %q, want %q", result, "is:retweet")
	}
}

func TestCompileAND(t *testing.T) {
	c := stream.NewRuleCompiler()
	node := model.ExprNode{AND: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](3)}}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "from:BBCPersian ایران" {
		t.Errorf("got %q", result)
	}
}

func TestCompileOR(t *testing.T) {
	c := stream.NewRuleCompiler()
	node := model.ExprNode{OR: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](2)}}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "(from:BBCPersian OR from:IranIntl)" {
		t.Errorf("got %q", result)
	}
}

func TestCompileNOTSingleSource(t *testing.T) {
	c := stream.NewRuleCompiler()
	node := model.ExprNode{NOT: &model.ExprNode{Src: ptr[int64](1)}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "-from:BBCPersian" {
		t.Errorf("got %q, want -from:BBCPersian", result)
	}
}

func TestCompileNOTOfOR(t *testing.T) {
	c := stream.NewRuleCompiler()
	node := model.ExprNode{NOT: &model.ExprNode{OR: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](2)}}}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "-(from:BBCPersian OR from:IranIntl)" {
		t.Errorf("got %q", result)
	}
}

func TestCompileNOTOfAND(t *testing.T) {
	c := stream.NewRuleCompiler()
	node := model.ExprNode{NOT: &model.ExprNode{AND: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](3)}}}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "-(from:BBCPersian ایران)" {
		t.Errorf("got %q", result)
	}
}

func TestCompileNOTOfInlineOp(t *testing.T) {
	c := stream.NewRuleCompiler()
	node := model.ExprNode{NOT: &model.ExprNode{Op: "is:retweet"}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "-is:retweet" {
		t.Errorf("got %q", result)
	}
}

func TestCompileComplex(t *testing.T) {
	c := stream.NewRuleCompiler()
	node := model.ExprNode{AND: []model.ExprNode{
		{OR: []model.ExprNode{{Src: ptr[int64](1)}, {Src: ptr[int64](2)}}},
		{Src: ptr[int64](4)},
		{NOT: &model.ExprNode{Op: "is:retweet"}},
	}}
	result, err := c.Compile(node, mockResolver(testSources))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "(from:BBCPersian OR from:IranIntl) lang:fa -is:retweet" {
		t.Errorf("got %q", result)
	}
}

func TestCompileMissingSource(t *testing.T) {
	c := stream.NewRuleCompiler()
	_, err := c.Compile(model.ExprNode{Src: ptr[int64](99)}, mockResolver(testSources))
	if err == nil {
		t.Error("expected error for missing source")
	}
}
