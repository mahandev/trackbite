package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Scale is the Go-side wrapper around the Swift `trackweighd` helper.
//
// The helper prints one of three line types to stdout:
//
//   READY <hz>             — emitted once at startup
//   READING <grams>        — emitted every frame (~90 Hz)
//   (anything else)        — ignored
//
// Scale parses those lines, keeps the latest reading in memory under an
// atomic so callers can ask for "current grams" without taking a lock,
// and exposes Tare() / Calibrate() / Close() to drive the helper via
// stdin.
type Scale struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	current atomic.Uint64 // bits of a float64; updated every frame

	smoothing []float64 // rolling buffer for low-pass filtering
	smoothMu  sync.Mutex

	closed atomic.Bool
}

// StartScale spawns the helper binary and begins streaming readings.
//
// Context cancellation kills the helper. The returned Scale is safe to
// use from multiple goroutines — Current() is lock-free, the rest are
// guarded internally.
func StartScale(ctx context.Context, helperPath string) (*Scale, error) {
	cmd := exec.CommandContext(ctx, helperPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// Stderr from the helper carries diagnostic lines like "warn: tare set: 0.42".
	// We pipe it straight to our own stderr so it shows up in logs.
	cmd.Stderr = stderrPassthrough{}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", helperPath, err)
	}

	s := &Scale{
		cmd:       cmd,
		stdin:     stdin,
		smoothing: make([]float64, 0, smoothingWindow),
	}
	go s.readLoop(stdout)
	return s, nil
}

// smoothingWindow controls the length of the moving-average filter we
// apply to incoming readings. Force Touch readings are noisy; averaging
// the last ~10 frames (~110 ms at 90 Hz) gives a stable display without
// noticeable lag. Higher = smoother but laggier.
const smoothingWindow = 10

func (s *Scale) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "READING "):
			val, err := strconv.ParseFloat(line[len("READING "):], 64)
			if err != nil {
				continue
			}
			smoothed := s.pushSmoothing(val)
			// Pack the float64 into 64 bits and store atomically. Readers
			// unpack with the inverse operation. No mutex needed.
			s.current.Store(float64ToBits(smoothed))
		case strings.HasPrefix(line, "READY"):
			// Helper signalled it's up. Nothing to do — we'll just start
			// receiving READINGs next.
		}
	}
}

func (s *Scale) pushSmoothing(v float64) float64 {
	s.smoothMu.Lock()
	defer s.smoothMu.Unlock()
	if len(s.smoothing) == smoothingWindow {
		s.smoothing = s.smoothing[1:]
	}
	s.smoothing = append(s.smoothing, v)
	var sum float64
	for _, x := range s.smoothing {
		sum += x
	}
	return sum / float64(len(s.smoothing))
}

// Current returns the latest smoothed weight reading in grams.
// Returns 0 if the helper hasn't emitted any frames yet.
func (s *Scale) Current() float64 {
	return bitsToFloat64(s.current.Load())
}

// Tare resets the helper's zero offset. The user should keep one finger
// on the trackpad with no food touching it when this is called.
func (s *Scale) Tare() error {
	_, err := io.WriteString(s.stdin, "TARE\n")
	return err
}

// Calibrate tells the helper to assume the current load is `grams` grams
// and recompute its force-to-grams scale factor accordingly.
func (s *Scale) Calibrate(grams float64) error {
	_, err := fmt.Fprintf(s.stdin, "CALIBRATE %g\n", grams)
	return err
}

// Close shuts the helper down cleanly.
func (s *Scale) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	io.WriteString(s.stdin, "QUIT\n")
	s.stdin.Close()
	return s.cmd.Wait()
}

// stderrPassthrough forwards every byte to the parent process's stderr.
// Implemented as a tiny io.Writer rather than just os.Stderr so we can
// prefix lines later if needed (e.g. "[trackweighd] ...").
type stderrPassthrough struct{}

func (stderrPassthrough) Write(p []byte) (int, error) {
	return fmt.Fprint(stderrTarget(), string(p))
}

// stderrTarget is a function so tests can replace it. The default is
// os.Stderr; production code never overrides it.
var stderrTarget = func() io.Writer { return defaultStderr }

// float64 <-> uint64 bit packing for lock-free atomic float updates.
// The standard library doesn't ship atomic.Float64, so we go through
// math.Float64bits via this thin wrapper to keep the imports lean.
func float64ToBits(f float64) uint64 { return mathFloat64bits(f) }
func bitsToFloat64(b uint64) float64 { return mathFloat64frombits(b) }
