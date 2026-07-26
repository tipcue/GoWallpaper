//go:build windows

package engine

import (
	"context"
	"time"
)

// maxPTSDrift is how far behind the media clock we tolerate before
// re-anchoring. Prevents a permanent lag spiral after a long GC pause or
// system sleep without aggressively skipping frames.
const maxPTSDrift = 500 * time.Millisecond

// maxReasonablePTS rejects nonsense timestamps (e.g. AV_NOPTS_VALUE after
// conversion) so we fall back to FPSLimit-only pacing.
const maxReasonablePTS = 24 * time.Hour

// Pacer schedules frame presentation from media PTS, with an optional
// maximum frame rate (MinFrame). Call Reset after Seek / loop restarts.
//
// Zero value is ready to use: set MinFrame before the first Wait if needed.
type Pacer struct {
	// MinFrame is the minimum interval between presents (from FPSLimit).
	// Zero means no rate cap.
	MinFrame time.Duration

	// Optional hooks for tests. nil ⇒ time.Now / cancellable real sleep.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration)

	anchored    bool
	wall0       time.Time
	pts0        time.Duration
	lastPresent time.Time
}

// Reset clears the media-clock anchor. Call after dec.Seek() so the next
// frame becomes the new origin.
func (p *Pacer) Reset() {
	p.anchored = false
	p.wall0 = time.Time{}
	p.pts0 = 0
	// Keep lastPresent so MinFrame still prevents a burst right after loop.
}

// Wait blocks until the frame with the given PTS should be presented, or
// until ctx is cancelled. Invalid PTS skips media-clock waits and only
// enforces MinFrame.
func (p *Pacer) Wait(ctx context.Context, pts time.Duration) {
	if ctx.Err() != nil {
		return
	}

	now := p.currentTime()
	wait := time.Duration(0)

	if p.MinFrame > 0 && !p.lastPresent.IsZero() {
		if d := p.MinFrame - now.Sub(p.lastPresent); d > 0 {
			wait = d
		}
	}

	if validPTS(pts) {
		if !p.anchored {
			p.anchored = true
			p.wall0 = now
			p.pts0 = pts
		} else {
			target := p.wall0.Add(pts - p.pts0)
			if d := target.Sub(now); d > wait {
				wait = d
			}
		}
	}

	if wait > 0 {
		p.doSleep(ctx, wait)
		now = p.currentTime()
	}

	// Soft re-sync when we are still far behind after waiting (or if we
	// skipped wait because we were already late).
	if validPTS(pts) && p.anchored {
		target := p.wall0.Add(pts - p.pts0)
		if now.After(target.Add(maxPTSDrift)) {
			p.wall0 = now
			p.pts0 = pts
		}
	}

	p.lastPresent = now
}

func (p *Pacer) currentTime() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

func (p *Pacer) doSleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	if p.sleep != nil {
		p.sleep(ctx, d)
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func validPTS(pts time.Duration) bool {
	return pts >= 0 && pts < maxReasonablePTS
}
