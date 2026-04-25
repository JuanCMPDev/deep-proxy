package auth

import (
	"sync"
	"testing"
)

func TestHeaderStore_GetSet(t *testing.T) {
	s := NewHeaderStore("t0", "c0", "h0")
	if tok, c, h := s.Get(); tok != "t0" || c != "c0" || h != "h0" {
		t.Errorf("initial: got (%q,%q,%q), want (t0,c0,h0)", tok, c, h)
	}

	s.Set("t1", "c1", "h1")
	if tok, c, h := s.Get(); tok != "t1" || c != "c1" || h != "h1" {
		t.Errorf("after Set: got (%q,%q,%q)", tok, c, h)
	}
}

func TestHeaderStore_PartialUpdatePreservesPrevious(t *testing.T) {
	s := NewHeaderStore("t0", "c0", "h0")
	// Update only cookie; token and hif preserved.
	s.Set("", "c1", "")
	if tok, c, h := s.Get(); tok != "t0" || c != "c1" || h != "h0" {
		t.Errorf("partial update: got (%q,%q,%q)", tok, c, h)
	}
	// All empty — no-op.
	s.Set("", "", "")
	if tok, c, h := s.Get(); tok != "t0" || c != "c1" || h != "h0" {
		t.Errorf("noop: got (%q,%q,%q)", tok, c, h)
	}
}

func TestHeaderStore_ConcurrentAccess(t *testing.T) {
	s := NewHeaderStore("t0", "c0", "h0")

	const N = 100
	var wg sync.WaitGroup
	wg.Add(N * 2)

	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			s.Set("t1", "c1", "h1")
		}()
		go func() {
			defer wg.Done()
			_, _, _ = s.Get()
		}()
	}
	wg.Wait()

	if tok, c, h := s.Get(); tok != "t1" || c != "c1" || h != "h1" {
		t.Errorf("final: got (%q,%q,%q)", tok, c, h)
	}
}
