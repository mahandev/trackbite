// scale-test — a standalone smoke test for the trackpad pressure pipeline.
//
// What it does:
//   1. Spawns the Swift `trackweighd` helper (built by `make helper`).
//   2. Streams every line the helper prints — READING, READY, warnings —
//      with a wall-clock timestamp so you can see exactly when readings
//      change relative to what you're doing with your hand.
//   3. Forwards a few commands from stdin to the helper:
//        t            → TARE       (recapture zero with finger on pad)
//        c <grams>    → CALIBRATE  (tell helper "current load is N grams")
//        v            → VERBOSE    (toggle per-touch diagnostic dump on stderr)
//        q / Ctrl-C   → quit
//
// The point isn't accuracy or smoothing — it's a binary "is the trackpad
// reporting any pressure at all, and does it change when I move things on
// the pad?" If raw READING values move here but the main app shows 0 g,
// the bug is in scale.go's smoothing or UI; if they don't move here, the
// problem is between the helper and the trackpad (permissions, hardware,
// or — most often — the missing finger; see README.md for the quirk).
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const defaultHelper = "trackweighd/bin/trackweighd"

func main() {
	helper := defaultHelper
	if len(os.Args) > 1 {
		helper = os.Args[1]
	}
	if _, err := os.Stat(helper); err != nil {
		fmt.Fprintf(os.Stderr, "helper not found at %s — run `make helper` first\n", helper)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	cmd := exec.CommandContext(ctx, helper)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fatal("stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fatal("stderr pipe: %v", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fatal("stdin pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		fatal("start helper: %v", err)
	}

	printBanner(helper)

	go copyStderr(stderr)
	go forwardStdin(stdin, cancel)

	scanner := bufio.NewScanner(stdout)
	start := time.Now()
	// Throttle READING lines to ~3 Hz so the screen is readable. Non-
	// READING lines (READY, warn:, etc.) always print so we never miss
	// helper diagnostics.
	const readingPrintInterval = 333 * time.Millisecond
	var lastReadingPrint time.Time
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "READING ") {
			if time.Since(lastReadingPrint) < readingPrintInterval {
				continue
			}
			lastReadingPrint = time.Now()
		}
		fmt.Printf("[%6.2fs] %s\n", time.Since(start).Seconds(), line)
	}
	_ = cmd.Wait()
}

func printBanner(helper string) {
	fmt.Println("─── trackweighd smoke test ───")
	fmt.Println("helper:", helper)
	fmt.Println()
	fmt.Println("Hardware quirk: the Force Touch trackpad only reports pressure")
	fmt.Println("while a *capacitive contact* (a finger) is on the pad. Food alone")
	fmt.Println("reads 0 g. To weigh, rest one finger on the pad and put the food")
	fmt.Println("next to your finger — the strain gauges sum both.")
	fmt.Println()
	fmt.Println("Walk through these stages and watch the READING numbers:")
	fmt.Println("  1. Hands off the laptop entirely      → expect READING 0.00")
	fmt.Println("  2. Rest ONE finger lightly on the pad → expect READING to rise")
	fmt.Println("  3. Type 't' + enter (tare)            → readings drop back near 0")
	fmt.Println("  4. Keep finger, set food next to it   → readings rise = food weight")
	fmt.Println("  5. Lift your finger, leave food       → readings collapse to 0 (quirk)")
	fmt.Println()
	fmt.Println("Commands (type + enter):")
	fmt.Println("  t            tare")
	fmt.Println("  c <grams>    calibrate to a known weight (e.g.  c 100)")
	fmt.Println("  v            toggle verbose per-touch dump (helper stderr)")
	fmt.Println("  q            quit")
	fmt.Println()
	fmt.Println("Streaming raw helper output...")
	fmt.Println()
}

func copyStderr(r io.Reader) { _, _ = io.Copy(os.Stderr, r) }

func forwardStdin(w io.WriteCloser, cancel context.CancelFunc) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			continue
		case line == "q", line == "quit", line == "exit":
			_, _ = w.Write([]byte("QUIT\n"))
			cancel()
			return
		case line == "t", line == "tare":
			_, _ = w.Write([]byte("TARE\n"))
			fmt.Println(">> sent TARE — keep one finger on the pad for ~250 ms")
		case line == "v", line == "verbose":
			_, _ = w.Write([]byte("VERBOSE\n"))
			fmt.Println(">> sent VERBOSE — per-touch dumps appear on stderr (~5 Hz)")
		case strings.HasPrefix(line, "c "):
			grams := strings.TrimSpace(line[2:])
			_, _ = w.Write([]byte("CALIBRATE " + grams + "\n"))
			fmt.Printf(">> sent CALIBRATE %s\n", grams)
		default:
			fmt.Printf(">> ignoring: %q (use t / c <grams> / q)\n", line)
		}
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
