package ado

import (
	"context"
	"testing"
)

func TestFakeCommentsPaginates(t *testing.T) {
	f := NewFake()
	f.Latency = 0

	comments, next, err := f.Comments(context.Background(), "Platform", 1003, "")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(comments) != 2 || next == "" {
		t.Fatalf("first page = %d comments, next=%q, want 2 and a token", len(comments), next)
	}
	if comments[len(comments)-1].Text != "Draft is up, PTAL @Priya Natarajan." {
		t.Errorf("newest comment = %+v", comments[len(comments)-1])
	}

	older, next2, err := f.Comments(context.Background(), "Platform", 1003, next)
	if err != nil {
		t.Fatalf("Comments (older): %v", err)
	}
	if len(older) != 1 || next2 != "" {
		t.Fatalf("older page = %d comments, next=%q, want 1 and no more", len(older), next2)
	}
	if older[0].Text != "Started on this — will push a draft today." {
		t.Errorf("oldest comment = %+v", older[0])
	}
}

func TestFakeCommentsEmpty(t *testing.T) {
	f := NewFake()
	f.Latency = 0

	comments, next, err := f.Comments(context.Background(), "Platform", 1002, "")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(comments) != 0 || next != "" {
		t.Errorf("comments = %+v next=%q, want none", comments, next)
	}
}
