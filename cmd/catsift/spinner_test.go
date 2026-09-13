package main

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSpinnerColorsFrameCyan(t *testing.T) {
	frame := "⠋"
	want := "\x1b[36m" + frame + "\x1b[39m"
	if got := colorSpinnerFrame(frame); got != want {
		t.Fatalf("colorSpinnerFrame() = %q, want %q", got, want)
	}
}

func TestSpinnerUsesBrailleFrames(t *testing.T) {
	want := "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	if got := strings.Join(spinnerFrames, ""); got != want {
		t.Fatalf("spinnerFrames = %q, want %q", got, want)
	}
}

func TestSpinnerLineFormatsElapsedTime(t *testing.T) {
	tests := []struct {
		name    string
		frame   string
		label   string
		elapsed time.Duration
		want    string
	}{
		{name: "under one second", frame: "|", label: "Reading Codex history", elapsed: 400 * time.Millisecond, want: "| Reading Codex history…"},
		{name: "seconds", frame: "/", label: "Reading Codex history", elapsed: 4*time.Second + 900*time.Millisecond, want: "/ Reading Codex history… 4s"},
		{name: "minutes", frame: "-", label: "Scanning installed skills", elapsed: 65 * time.Second, want: "- Scanning installed skills… 1m05s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := spinnerLine(tt.frame, tt.label, tt.elapsed); got != tt.want {
				t.Fatalf("spinnerLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatSpinnerElapsedUsesCoarseUnits(t *testing.T) {
	tests := []struct {
		elapsed time.Duration
		want    string
	}{
		{elapsed: 704 * time.Millisecond, want: "<1s"},
		{elapsed: 1500 * time.Millisecond, want: "1s"},
		{elapsed: 69 * time.Second, want: "1m09s"},
	}
	for _, tt := range tests {
		if got := formatSpinnerElapsed(tt.elapsed); got != tt.want {
			t.Errorf("formatSpinnerElapsed(%s) = %q, want %q", tt.elapsed, got, tt.want)
		}
	}
}

func TestSpinnerWritesDiagnosticsOutsideCurrentLine(t *testing.T) {
	var output recordingWriter
	spinner := newSpinner(&output, true, false)
	spinner.lineWidth = 5

	spinner.writeDiagnostic(func() {
		_, _ = io.WriteString(&output, "debug: loading\n")
	})

	if got, want := output.String(), "\r     \rdebug: loading\n"; got != want {
		t.Fatalf("diagnostic output = %q, want %q", got, want)
	}
	if spinner.lineWidth != 0 {
		t.Fatalf("spinner line width = %d, want 0", spinner.lineWidth)
	}
}

func TestSpinnerDoesNotRenderWhenDisabled(t *testing.T) {
	var output recordingWriter
	spinner := newSpinner(&output, false, false)
	stop := spinner.Start("Reading Codex history")
	stop()

	if got := output.String(); got != "" {
		t.Fatalf("disabled spinner output = %q, want empty", got)
	}
}

func TestStartProgressPhaseStopsPreviousPhase(t *testing.T) {
	var events []string
	stopPrevious := func() { events = append(events, "stop previous") }
	start := func(label string) func() {
		events = append(events, "start "+label)
		return func() { events = append(events, "stop current") }
	}

	stopCurrent := startProgressPhase(stopPrevious, start, "Scanning installed skills")
	stopCurrent()

	want := []string{"stop previous", "start Scanning installed skills", "stop current"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("startProgressPhase events = %#v, want %#v", events, want)
	}
}

func TestSpinnerDoesNotRenderAtDelayBoundaryWhenStopped(t *testing.T) {
	var output recordingWriter
	start := time.Date(2026, time.September, 4, 0, 0, 0, 0, time.UTC)
	delay := make(chan time.Time, 1)
	delay <- start
	tickerCreated := false

	spinner := newSpinner(&output, true, false)
	spinner.clock = spinnerClock{
		now:   func() time.Time { return start },
		after: func(time.Duration) <-chan time.Time { return delay },
		ticker: func(time.Duration) *spinnerTicker {
			tickerCreated = true
			return &spinnerTicker{C: make(chan time.Time), stop: func() {}}
		},
	}

	stop := spinner.Start("Reading Codex history")
	stop()

	if got := output.String(); got != "" {
		t.Fatalf("spinner output at delay boundary = %q, want empty", got)
	}
	if tickerCreated {
		t.Fatal("spinner created a ticker after it was stopped")
	}
}

func TestSpinnerWaitsBeforeRenderingAndClearsLine(t *testing.T) {
	output := recordingWriter{writes: make(chan struct{}, 16)}
	delay := make(chan time.Time, 1)
	ticks := make(chan time.Time, 1)
	afterCalled := make(chan struct{})
	start := time.Date(2026, time.September, 4, 0, 0, 0, 0, time.UTC)
	now := start
	tickerStopped := false

	spinner := newSpinner(&output, true, true)
	spinner.clock = spinnerClock{
		now: func() time.Time { return now },
		after: func(time.Duration) <-chan time.Time {
			close(afterCalled)
			return delay
		},
		ticker: func(time.Duration) *spinnerTicker {
			return &spinnerTicker{C: ticks, stop: func() { tickerStopped = true }}
		},
	}

	stop := spinner.Start("Reading Codex history")
	select {
	case <-afterCalled:
	case <-time.After(time.Second):
		t.Fatal("spinner did not start its delay timer")
	}
	select {
	case <-output.writes:
		t.Fatal("spinner rendered before its delay elapsed")
	default:
	}

	now = start.Add(4*time.Second + 900*time.Millisecond)
	delay <- now
	select {
	case <-output.writes:
	case <-time.After(time.Second):
		t.Fatal("spinner did not render after its delay elapsed")
	}

	line := spinnerLine(spinnerFrames[0], "Reading Codex history", 4*time.Second+900*time.Millisecond)
	coloredLine := spinnerLine(colorSpinnerFrame(spinnerFrames[0]), "Reading Codex history", 4*time.Second+900*time.Millisecond)
	if got := output.String(); !strings.Contains(got, "\r"+coloredLine) {
		t.Fatalf("spinner output = %q, want line %q", got, line)
	}

	stop()
	clearSuffix := "\r" + strings.Repeat(" ", len([]rune(line))) + "\r"
	if got := output.String(); !strings.HasSuffix(got, clearSuffix) {
		t.Fatalf("stopped spinner output = %q, want suffix %q", got, clearSuffix)
	}
	if !tickerStopped {
		t.Fatal("spinner ticker was not stopped")
	}
}

type recordingWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	writes chan struct{}
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buffer.Write(p)
	w.mu.Unlock()

	select {
	case w.writes <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (w *recordingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}
