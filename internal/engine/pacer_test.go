//go:build windows

package engine

import (
	"context"
	"testing"
	"time"
)

// fakeClock advances only when tests bump now, and records sleep requests.
type fakeClock struct {
	now    time.Time
	slept  []time.Duration
	cancel context.CancelFunc
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) attach(p *Pacer) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	p.now = func() time.Time { return c.now }
	p.sleep = func(ctx context.Context, d time.Duration) {
		c.slept = append(c.slept, d)
		// Simulate time passing unless cancelled before sleep starts.
		if ctx.Err() != nil {
			return
		}
		c.now = c.now.Add(d)
	}
	return ctx
}

func TestPacer_MinFrameOnly_InvalidPTS(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	p := &Pacer{MinFrame: 30 * time.Millisecond}
	ctx := clk.attach(p)

	p.Wait(ctx, -1) // invalid → no media wait, no min on first frame
	if len(clk.slept) != 0 {
		t.Fatalf("first frame slept %v, want none", clk.slept)
	}

	p.Wait(ctx, -1)
	if len(clk.slept) != 1 || clk.slept[0] != 30*time.Millisecond {
		t.Fatalf("second frame slept %v, want [30ms]", clk.slept)
	}
}

func TestPacer_PTSSchedule(t *testing.T) {
	clk := newFakeClock(time.Unix(100, 0))
	p := &Pacer{} // no FPS cap
	ctx := clk.attach(p)

	p.Wait(ctx, 0)
	p.Wait(ctx, 40*time.Millisecond)
	if len(clk.slept) != 1 || clk.slept[0] != 40*time.Millisecond {
		t.Fatalf("slept %v, want [40ms] for PTS delta", clk.slept)
	}

	// Next frame at 100ms media time; wall already advanced 40ms → wait 60ms.
	p.Wait(ctx, 100*time.Millisecond)
	if len(clk.slept) != 2 || clk.slept[1] != 60*time.Millisecond {
		t.Fatalf("slept %v, want last wait 60ms", clk.slept)
	}
}

func TestPacer_FPSLimitCapsFasterPTS(t *testing.T) {
	// Content wants 10ms frames; cap is 30ms → MinFrame wins.
	clk := newFakeClock(time.Unix(0, 0))
	p := &Pacer{MinFrame: 30 * time.Millisecond}
	ctx := clk.attach(p)

	p.Wait(ctx, 0)
	p.Wait(ctx, 10*time.Millisecond)
	if len(clk.slept) != 1 || clk.slept[0] != 30*time.Millisecond {
		t.Fatalf("slept %v, want [30ms] (FPS cap over PTS)", clk.slept)
	}
}

func TestPacer_PTSSlowerThanCap(t *testing.T) {
	// Cap 30ms but PTS delta 100ms → wait for PTS.
	clk := newFakeClock(time.Unix(0, 0))
	p := &Pacer{MinFrame: 30 * time.Millisecond}
	ctx := clk.attach(p)

	p.Wait(ctx, 0)
	p.Wait(ctx, 100*time.Millisecond)
	if len(clk.slept) != 1 || clk.slept[0] != 100*time.Millisecond {
		t.Fatalf("slept %v, want [100ms]", clk.slept)
	}
}

func TestPacer_ResetReanchors(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	p := &Pacer{}
	ctx := clk.attach(p)

	p.Wait(ctx, 0)
	p.Wait(ctx, 50*time.Millisecond)
	if len(clk.slept) != 1 {
		t.Fatalf("pre-reset slept %v", clk.slept)
	}

	p.Reset()
	// After loop, media PTS restarts at 0; should not wait ~(-50ms) or huge gap.
	p.Wait(ctx, 0)
	if len(clk.slept) != 1 {
		t.Fatalf("after reset first frame slept %v, want no new sleep", clk.slept)
	}
	p.Wait(ctx, 50*time.Millisecond)
	if len(clk.slept) != 2 || clk.slept[1] != 50*time.Millisecond {
		t.Fatalf("after reset slept %v, want second wait 50ms", clk.slept)
	}
}

func TestPacer_LateReanchors(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	p := &Pacer{}
	ctx := clk.attach(p)

	p.Wait(ctx, 0)
	// Jump wall clock far ahead without sleeping (decode stall).
	clk.now = clk.now.Add(2 * time.Second)
	clk.slept = nil
	p.Wait(ctx, 33*time.Millisecond) // should re-anchor, not sleep negative
	if len(clk.slept) != 0 {
		t.Fatalf("late frame slept %v, want none", clk.slept)
	}
	// After re-anchor, next delta is relative to new origin.
	p.Wait(ctx, 33*time.Millisecond+40*time.Millisecond)
	if len(clk.slept) != 1 || clk.slept[0] != 40*time.Millisecond {
		t.Fatalf("post re-anchor slept %v, want [40ms]", clk.slept)
	}
}

func TestPacer_ContextCancelSkipsSleepSideEffects(t *testing.T) {
	clk := newFakeClock(time.Unix(0, 0))
	p := &Pacer{MinFrame: time.Second}
	ctx := clk.attach(p)
	p.Wait(ctx, 0)

	clk.cancel()
	before := clk.now
	p.Wait(ctx, 0) // cancelled: sleep hook returns without advancing if we check ctx
	// Our fake sleep still records but does not advance when ctx done.
	if !clk.now.Equal(before) {
		// Allow either no sleep call or sleep with no advance; clock must not jump a full second.
		if clk.now.Sub(before) >= time.Second {
			t.Fatalf("clock advanced by %v after cancel", clk.now.Sub(before))
		}
	}
}

func TestValidPTS(t *testing.T) {
	if !validPTS(0) || !validPTS(time.Second) {
		t.Fatal("expected non-negative short PTS valid")
	}
	if validPTS(-time.Millisecond) {
		t.Fatal("negative PTS should be invalid")
	}
	if validPTS(maxReasonablePTS) {
		t.Fatal("PTS at maxReasonablePTS should be invalid")
	}
}
