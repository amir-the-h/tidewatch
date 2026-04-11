package store

import (
	"context"
	"testing"
)

func TestGeocodeCache_SetAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.SetGeocodeCache(ctx, 35.6892, 51.389, "Tehran, Iran"); err != nil {
		t.Fatalf("SetGeocodeCache: %v", err)
	}

	name, found, err := s.GetGeocodeCache(ctx, 35.6892, 51.389)
	if err != nil {
		t.Fatalf("GetGeocodeCache: %v", err)
	}
	if !found {
		t.Fatal("want found=true")
	}
	if name != "Tehran, Iran" {
		t.Errorf("want 'Tehran, Iran', got %q", name)
	}
}

func TestGeocodeCache_Miss(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, found, err := s.GetGeocodeCache(ctx, 0, 0)
	if err != nil {
		t.Fatalf("GetGeocodeCache: %v", err)
	}
	if found {
		t.Error("want found=false for missing entry")
	}
}

func TestGeocodeCache_Upsert(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_ = s.SetGeocodeCache(ctx, 35.0, 51.0, "first")
	_ = s.SetGeocodeCache(ctx, 35.0, 51.0, "updated")

	name, found, err := s.GetGeocodeCache(ctx, 35.0, 51.0)
	if err != nil {
		t.Fatalf("GetGeocodeCache: %v", err)
	}
	if !found || name != "updated" {
		t.Errorf("want 'updated', got %q (found=%v)", name, found)
	}
}

func TestGeocodeCache_EmptyNameCached(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Empty string = confirmed "no result" — must be distinguishable from "not in cache".
	_ = s.SetGeocodeCache(ctx, 10.0, 10.0, "")

	name, found, err := s.GetGeocodeCache(ctx, 10.0, 10.0)
	if err != nil {
		t.Fatalf("GetGeocodeCache: %v", err)
	}
	if !found {
		t.Fatal("want found=true for cached empty name")
	}
	if name != "" {
		t.Errorf("want empty name, got %q", name)
	}
}
