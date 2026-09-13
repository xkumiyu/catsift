package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	spinnerDelay    = 400 * time.Millisecond
	spinnerInterval = 100 * time.Millisecond
	spinnerCyan     = "\x1b[36m"
	spinnerReset    = "\x1b[39m"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type spinnerTicker struct {
	C    <-chan time.Time
	stop func()
}

func (t *spinnerTicker) Stop() {
	if t != nil && t.stop != nil {
		t.stop()
	}
}

type spinnerClock struct {
	now    func() time.Time
	after  func(time.Duration) <-chan time.Time
	ticker func(time.Duration) *spinnerTicker
}

type spinner struct {
	out       io.Writer
	enabled   bool
	delay     time.Duration
	interval  time.Duration
	clock     spinnerClock
	colored   bool
	mutex     sync.Mutex
	lineWidth int
}

func newSpinner(out io.Writer, enabled, colored bool) *spinner {
	return &spinner{
		out:      out,
		enabled:  enabled && out != nil,
		delay:    spinnerDelay,
		interval: spinnerInterval,
		colored:  colored && enabled && out != nil,
		clock: spinnerClock{
			now:   time.Now,
			after: time.After,
			ticker: func(interval time.Duration) *spinnerTicker {
				ticker := time.NewTicker(interval)
				return &spinnerTicker{C: ticker.C, stop: ticker.Stop}
			},
		},
	}
}

func (s *spinner) Start(label string) func() {
	if !s.enabled {
		return func() {}
	}

	done := make(chan struct{})
	stopped := make(chan struct{})
	go s.run(label, done, stopped)

	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-stopped
	}
}

func startProgressPhase(stopPrevious func(), start func(string) func(), label string) func() {
	stopPrevious()
	return start(label)
}

func (s *spinner) run(label string, done <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)

	startedAt := s.clock.now()
	select {
	case <-s.clock.after(s.delay):
		select {
		case <-done:
			return
		default:
		}
	case <-done:
		return
	}

	ticker := s.clock.ticker(s.interval)
	defer ticker.Stop()

	frame := 0
	draw := func(now time.Time) {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		frameText := spinnerFrames[frame%len(spinnerFrames)]
		elapsed := now.Sub(startedAt)
		visibleLine := spinnerLine(frameText, label, elapsed)
		line := visibleLine
		if s.colored {
			line = spinnerLine(colorSpinnerFrame(frameText), label, elapsed)
		}
		prefix := "\r"
		if s.lineWidth > 0 {
			prefix = fmt.Sprintf("\r%s\r", strings.Repeat(" ", s.lineWidth))
		}
		_, _ = io.WriteString(s.out, prefix+line)
		s.lineWidth = len([]rune(visibleLine))
		frame++
	}

	draw(s.clock.now())
	for {
		select {
		case now, ok := <-ticker.C:
			if !ok {
				s.clearCurrent()
				return
			}
			draw(now)
		case <-done:
			s.clearCurrent()
			return
		}
	}
}

func (s *spinner) clearCurrent() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.lineWidth == 0 {
		return
	}
	_, _ = fmt.Fprintf(s.out, "\r%s\r", strings.Repeat(" ", s.lineWidth))
	s.lineWidth = 0
}

func (s *spinner) writeDiagnostic(write func()) {
	if !s.enabled {
		write()
		return
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.clearCurrentLocked()
	write()
}

func (s *spinner) clearCurrentLocked() {
	if s.lineWidth == 0 {
		return
	}
	_, _ = fmt.Fprintf(s.out, "\r%s\r", strings.Repeat(" ", s.lineWidth))
	s.lineWidth = 0
}

func spinnerLine(frame, label string, elapsed time.Duration) string {
	line := fmt.Sprintf("%s %s…", frame, label)
	if elapsed >= time.Second {
		line += " " + formatSpinnerElapsed(elapsed)
	}
	return line
}

func colorSpinnerFrame(frame string) string {
	return spinnerCyan + frame + spinnerReset
}

func formatSpinnerElapsed(elapsed time.Duration) string {
	if elapsed < time.Second {
		return "<1s"
	}
	seconds := int(elapsed / time.Second)
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
}
