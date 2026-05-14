package main

import (
	"fmt"
	"strings"
)

// Tiny ANSI-escape helpers so main.go doesn't have to think about
// escape codes. We deliberately use raw strings here — no third-party
// TUI library, no terminal probing. Works in any modern macOS terminal.

const (
	ansiClearLine  = "\033[2K"   // erase the line under the cursor
	ansiHome       = "\r"         // move cursor to column 0
	ansiHideCursor = "\033[?25l"
	ansiShowCursor = "\033[?25h"
	ansiBold       = "\033[1m"
	ansiDim        = "\033[2m"
	ansiReset      = "\033[0m"
	ansiGreen      = "\033[32m"
	ansiYellow     = "\033[33m"
	ansiCyan       = "\033[36m"
	ansiRed        = "\033[31m"
)

// drawStatus redraws the single-line status above the prompt. Called
// every ~100 ms with the latest weight and the most recent barcode (if
// any). Uses \r + clear-line so consecutive frames overwrite each other
// instead of scrolling.
func drawStatus(grams float64, barcode, hint string) {
	parts := []string{
		fmt.Sprintf("%sweight%s %s%6.1f g%s", ansiDim, ansiReset, ansiCyan, grams, ansiReset),
	}
	if barcode != "" {
		parts = append(parts, fmt.Sprintf("%sbarcode%s %s%s%s",
			ansiDim, ansiReset, ansiYellow, barcode, ansiReset))
	}
	if hint != "" {
		parts = append(parts, fmt.Sprintf("%s%s%s", ansiDim, hint, ansiReset))
	}
	fmt.Print(ansiHome + ansiClearLine + strings.Join(parts, "   "))
}

// printResult breaks out of the status line and prints a multi-line
// calorie summary. Adds a leading newline so we don't overwrite the
// status line that was active before.
func printResult(info *NutritionInfo, grams, kcal float64) {
	name := info.Name
	if info.Brand != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(info.Brand)) {
		name = info.Brand + " — " + name
	}
	fmt.Print("\n\n")
	fmt.Printf("  %s✓ %s%s\n", ansiGreen, name, ansiReset)
	fmt.Printf("  %s%.0f kcal%s for %s%.1f g%s   (%.0f kcal / 100 g)\n",
		ansiBold, kcal, ansiReset, ansiBold, grams, ansiReset, info.KcalPer100g)
	if info.ProteinPer100g+info.CarbsPer100g+info.FatPer100g > 0 {
		p := info.ProteinPer100g * grams / 100
		c := info.CarbsPer100g * grams / 100
		f := info.FatPer100g * grams / 100
		fmt.Printf("  %sprotein %.1f g · carbs %.1f g · fat %.1f g%s\n",
			ansiDim, p, c, f, ansiReset)
	}
	fmt.Printf("  %ssource: %s%s\n\n", ansiDim, info.Source, ansiReset)
}

func printError(msg string) {
	fmt.Printf("\n  %s× %s%s\n\n", ansiRed, msg, ansiReset)
}

func printBanner() {
	fmt.Print(ansiBold + "\n  trackbite" + ansiReset + ansiDim +
		" — trackpad scale + webcam barcode → calories\n" + ansiReset)
	fmt.Println()
	fmt.Println("  Place a finger lightly on the trackpad with food next to it.")
	fmt.Println("  Hold a barcode in front of the camera — or type a food name.")
	fmt.Println()
	fmt.Println(ansiDim + "  commands:" + ansiReset)
	fmt.Println(ansiDim + "    [enter]        reset and scan again" + ansiReset)
	fmt.Println(ansiDim + "    t              tare (zero) the scale" + ansiReset)
	fmt.Println(ansiDim + "    c <grams>      calibrate scale to known weight" + ansiReset)
	fmt.Println(ansiDim + "    n <food>       look up by name (e.g. \"n dal makhani\")" + ansiReset)
	fmt.Println(ansiDim + "    q              quit" + ansiReset)
	fmt.Println()
}
