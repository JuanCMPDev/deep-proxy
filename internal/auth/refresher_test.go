package auth

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefresher_StartFailsOnInitialRefreshError(t *testing.T) {
	store := NewHeaderStore("seed-tok", "seed", "seed")
	stubErr := errors.New("chrome unavailable")
	r := NewRefresher(store, func(_ context.Context) (string, string, string, error) {
		return "", "", "", stubErr
	}, 100*time.Millisecond)

	err := r.Start(context.Background())
	if err == nil {
		t.Fatal("expected initial refresh to fail")
	}
	if !errors.Is(err, stubErr) {
		t.Errorf("error chain should contain stubErr, got: %v", err)
	}
	// Store should keep seed values.
	if tok, c, h := store.Get(); tok != "seed-tok" || c != "seed" || h != "seed" {
		t.Errorf("store mutated on failure: got (%q,%q,%q)", tok, c, h)
	}
}

func TestRefresher_PeriodicallyUpdatesStore(t *testing.T) {
	store := NewHeaderStore("seed", "seed", "seed")
	var calls atomic.Int32

	r := NewRefresher(store, func(_ context.Context) (string, string, string, error) {
		n := calls.Add(1)
		s := "v" + intToStr(int(n))
		return s, s, s, nil
	}, 50*time.Millisecond)

	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer r.Stop()

	if _, c, _ := store.Get(); c != "v1" {
		t.Errorf("after Start: cookie=%q, want v1", c)
	}

	time.Sleep(120 * time.Millisecond)
	if _, c, _ := store.Get(); c == "v1" {
		t.Errorf("expected periodic update; cookie still %q", c)
	}
}

func TestRefresher_StopIsIdempotent(t *testing.T) {
	r := NewRefresher(NewHeaderStore("", "", ""), func(context.Context) (string, string, string, error) {
		return "t", "c", "h", nil
	}, time.Hour)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	r.Stop()
	r.Stop()
}

func TestRefresher_StopWithoutStart(t *testing.T) {
	r := NewRefresher(NewHeaderStore("", "", ""), nil, time.Hour)
	r.Stop()
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	out := ""
	for n > 0 {
		out = string(rune('0'+n%10)) + out
		n /= 10
	}
	return out
}
