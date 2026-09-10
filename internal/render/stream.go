package render

import (
	"io"
	"time"
)

// Stream owns the two timing promises the UX contract makes: something is printed
// within a second of start, nothing goes more than a few seconds without output
// while running, and no more than two frames a second are drawn (D2.1, D2.7).
//
// The clock is injected, so a replay at 50x is subject to the same budget as a
// live run and the tests can assert it without sleeping.
type Stream struct {
	Out      io.Writer
	Renderer Renderer
	Now      func() time.Time
	Width    func() int

	// MinInterval caps the render rate. Two a second is fast enough to feel live
	// and slow enough not to flicker.
	MinInterval time.Duration
	// Heartbeat is the longest silence allowed while something is still running.
	Heartbeat time.Duration

	started  time.Time
	lastAt   time.Time
	lastOut  string
	renders  int
	firstAt  time.Time
	maxSilen time.Duration
}

const (
	DefaultMinInterval = 500 * time.Millisecond
	DefaultHeartbeat   = 5 * time.Second
)

func (s *Stream) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Stream) width() int {
	if s.Width != nil {
		return s.Width()
	}
	return DefaultWidth
}

// Update renders if there is something new to say and the budget allows it.
func (s *Stream) Update(v View) error {
	now := s.now()
	if s.started.IsZero() {
		s.started = now
	}
	minInterval, heartbeat := s.MinInterval, s.Heartbeat
	if minInterval == 0 {
		minInterval = DefaultMinInterval
	}
	if heartbeat == 0 {
		heartbeat = DefaultHeartbeat
	}

	first := s.renders == 0
	silent := now.Sub(s.lastAt)
	if !first && silent < minInterval {
		return nil
	}

	out, changed := s.Renderer.Render(v, s.width())
	// The heartbeat exists so a long phase never looks hung. Nothing is redrawn
	// identically: the elapsed time in the view moves, so a heartbeat frame always
	// differs from the one before it.
	if !changed && !first && silent < heartbeat {
		return nil
	}
	if !changed && !first {
		return nil
	}

	if !s.lastAt.IsZero() && silent > s.maxSilen {
		s.maxSilen = silent
	}
	s.lastAt = now
	if first {
		s.firstAt = now
	}
	s.renders++
	s.lastOut = out
	if out == "" {
		return nil
	}
	_, err := io.WriteString(s.Out, out)
	return err
}

// Renders is how many frames were drawn, for the render-budget test.
func (s *Stream) Renders() int { return s.renders }

// TimeToFirstOutput is how long the user waited to see anything.
func (s *Stream) TimeToFirstOutput() time.Duration {
	if s.firstAt.IsZero() {
		return 0
	}
	return s.firstAt.Sub(s.started)
}

// LongestSilence is the largest gap between two frames.
func (s *Stream) LongestSilence() time.Duration { return s.maxSilen }

// Last is the most recent output, for tests that assert on the final frame.
func (s *Stream) Last() string { return s.lastOut }
