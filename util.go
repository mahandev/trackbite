package main

import (
	"io"
	"math"
	"os"
)

// Tiny utility layer. Centralising these lets scale.go / camera.go etc.
// stay focused on their own logic, and makes them easier to read for
// someone learning Go.

var defaultStderr io.Writer = os.Stderr

// Thin wrappers so scale.go doesn't need to import "math" just for the
// bit-pack helpers it uses to do lock-free float atomics.
func mathFloat64bits(f float64) uint64     { return math.Float64bits(f) }
func mathFloat64frombits(b uint64) float64 { return math.Float64frombits(b) }
