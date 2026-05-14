package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// main wires the four subsystems — config, scale, camera, nutrition
// chain — into a single interactive loop.
//
// Flow:
//   1. Load .env into Config.
//   2. Open the on-disk cache.
//   3. Build the source chain (OFF + USDA + IFCT).
//   4. Start the scale helper (if the binary exists).
//   5. Start the camera (if OpenCV/AVFoundation cooperate).
//   6. Loop: refresh the live status, watch the barcode channel, accept
//      stdin commands until the user types `q` or hits Ctrl-C.
//
// We treat the scale and camera as *optional* — the app degrades to a
// keyboard-only lookup tool if either fails, instead of hard-erroring.
// That makes development much smoother (you can iterate on the
// nutrition layer without having to fight camera permissions).

func main() {
	cfg, err := LoadConfig(".env")
	if err != nil {
		fatal("load config: %v", err)
	}

	cache, err := OpenCache(cfg.CacheDBPath)
	if err != nil {
		fatal("open cache: %v", err)
	}

	ifct, err := NewIFCT()
	if err != nil {
		fatal("load IFCT: %v", err)
	}

	// Order matters: Open Food Facts first for barcodes; IFCT first for
	// names (handled implicitly because OFF/USDA return ErrNotFound for
	// queries they don't support).
	chain := NewChain(cache,
		NewOpenFoodFacts(),
		ifct,
		NewUSDA(cfg.USDAKey),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SIGINT (Ctrl-C) cancels the root context; both helpers and the
	// camera see the cancellation and tear down cleanly.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	scale := tryStartScale(ctx, cfg.HelperBin)
	if scale != nil {
		defer scale.Close()
	}

	barcodes := make(chan string, 8)
	cam := tryStartCamera(ctx, cfg.CameraIndex, barcodes)
	if cam != nil {
		defer cam.Close()
	}

	printBanner()
	if scale == nil {
		fmt.Println(ansiYellow + "  (scale unavailable — readings will show 0g; type 'n <food>' to look up by name)" + ansiReset)
	}
	if cam == nil {
		fmt.Println(ansiYellow + "  (camera unavailable — barcode scanning disabled; type 'n <food>' to look up by name)" + ansiReset)
	}
	fmt.Print(ansiHideCursor)
	defer fmt.Print(ansiShowCursor)

	runLoop(ctx, cancel, scale, cam, chain, barcodes)
}

// tryStartScale attempts to spawn the helper binary. On failure (file
// missing, exec denied, etc.) we print a warning and return nil — the
// app keeps running, just without live weight readings.
func tryStartScale(ctx context.Context, helperBin string) *Scale {
	if _, err := os.Stat(helperBin); err != nil {
		fmt.Fprintf(os.Stderr, "warn: trackweighd binary not found at %s — run `make helper`\n", helperBin)
		return nil
	}
	s, err := StartScale(ctx, helperBin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: scale helper failed to start: %v\n", err)
		return nil
	}
	return s
}

// tryStartCamera attempts to open the webcam. Same degradation strategy
// as the scale.
func tryStartCamera(ctx context.Context, idx int, out chan string) *Camera {
	c, err := StartCamera(ctx, idx, out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: camera failed to start: %v\n", err)
		return nil
	}
	return c
}

// runLoop is the heart of the app. Three things run concurrently:
//
//   - A 100 ms ticker redraws the status line with the latest weight.
//   - The barcode channel delivers scanned codes from the camera.
//   - A stdin reader goroutine parses user commands.
//
// We use a single select so the loop stays single-threaded — no locks
// around the UI state.
func runLoop(
	ctx context.Context,
	cancel context.CancelFunc,
	scale *Scale,
	cam *Camera,
	chain *Chain,
	barcodes <-chan string,
) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	cmds := make(chan string, 8)
	go readStdin(cmds)

	var lastBarcode string
	var lastFood *NutritionInfo // most recent lookup; drives live kcal in status
	var hint string
	const idleHint = "type a command and press enter"

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			grams := 0.0
			if scale != nil {
				grams = scale.Current()
			}
			h := hint
			if h == "" {
				h = idleHint
			}
			drawStatus(grams, lastBarcode, lastFood, h)

		case code := <-barcodes:
			lastBarcode = code
			grams := 0.0
			if scale != nil {
				grams = scale.Current()
			}
			hint = "looking up barcode..."
			drawStatus(grams, lastBarcode, lastFood, hint)
			if info := lookupBarcode(ctx, chain, code, grams); info != nil {
				lastFood = info
			}
			hint = "press enter to scan another"

		case line := <-cmds:
			hint = handleCommand(ctx, line, scale, cam, chain, &lastBarcode, &lastFood, cancel)
		}
	}
}

func handleCommand(
	ctx context.Context,
	line string,
	scale *Scale,
	cam *Camera,
	chain *Chain,
	lastBarcode *string,
	lastFood **NutritionInfo,
	cancel context.CancelFunc,
) string {
	line = strings.TrimSpace(line)
	switch {
	case line == "" || line == "r":
		if cam != nil {
			cam.Reset()
		}
		*lastBarcode = ""
		*lastFood = nil
		return ""

	case line == "q" || line == "quit" || line == "exit":
		cancel()
		return "quitting..."

	case line == "t" || line == "tare":
		if scale == nil {
			printError("scale unavailable")
			return ""
		}
		if err := scale.Tare(); err != nil {
			printError("tare failed: " + err.Error())
		}
		return "tared — keep one finger on the pad"

	case strings.HasPrefix(line, "c "):
		if scale == nil {
			printError("scale unavailable")
			return ""
		}
		g, err := strconv.ParseFloat(strings.TrimSpace(line[2:]), 64)
		if err != nil || g <= 0 {
			printError("usage: c <grams>  (e.g. c 100)")
			return ""
		}
		if err := scale.Calibrate(g); err != nil {
			printError("calibrate failed: " + err.Error())
			return ""
		}
		return fmt.Sprintf("calibrated to %.1f g", g)

	case strings.HasPrefix(line, "n "):
		query := strings.TrimSpace(line[2:])
		if query == "" {
			printError("usage: n <food name>")
			return ""
		}
		grams := 0.0
		if scale != nil {
			grams = scale.Current()
		}
		if info := lookupName(ctx, chain, query, grams); info != nil {
			*lastFood = info
		}
		return "press enter to scan / look up again"

	default:
		printError("unknown command: " + line)
		return ""
	}
}

func lookupBarcode(ctx context.Context, chain *Chain, code string, grams float64) *NutritionInfo {
	info, err := chain.Barcode(ctx, code)
	if err != nil {
		fmt.Print("\n")
		printError(fmt.Sprintf("barcode %s not found in any source — try `n <food name>`", code))
		return nil
	}
	printResult(info, grams, KcalForGrams(info, grams))
	return info
}

func lookupName(ctx context.Context, chain *Chain, query string, grams float64) *NutritionInfo {
	info, err := chain.Name(ctx, query)
	if err != nil {
		fmt.Print("\n")
		printError("no match for \"" + query + "\"")
		return nil
	}
	printResult(info, grams, KcalForGrams(info, grams))
	return info
}

// readStdin pushes each line typed by the user onto the cmds channel.
// We use bufio.Scanner — line-buffered input is fine for our purposes;
// we don't need raw single-key reads.
func readStdin(cmds chan<- string) {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		cmds <- scanner.Text()
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
