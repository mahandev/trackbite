//
// trackweighd — reads raw Force Touch pressure and prints grams to stdout.
//
// ──────────────────────────────────────────────────────────────────────────
// How a MacBook trackpad reports "force"
// ──────────────────────────────────────────────────────────────────────────
// Apple's Force Touch trackpads (2015+ MBP, all Apple Silicon Airs/Pros)
// have four strain gauges under the surface that measure how hard the pad
// is being pressed. The pressure reading is exposed through the private
// framework `MultitouchSupport.framework`. Each contact event includes a
// `total.force` field — a unitless float that scales roughly linearly with
// applied force within the trackpad's usable range.
//
// One critical hardware quirk: the trackpad only reports pressure while a
// capacitive contact (a finger) is touching it. A dry, non-conductive
// object on its own registers zero. To weigh food, you keep one finger
// lightly on the pad and put the food next to your finger — the strain
// gauges measure the combined weight, and you subtract a tared offset
// captured while only your finger was touching.
//
// ──────────────────────────────────────────────────────────────────────────
// Protocol — what this binary prints
// ──────────────────────────────────────────────────────────────────────────
// One line per frame, ~30–120 Hz depending on the OS:
//
//     READING <grams>
//
// On startup, before any READING lines:
//
//     READY <samplesPerSec>
//
// On stdin we accept simple commands (one per line):
//
//     TARE                  — recapture the zero point from the next ~30
//                             frames (user should have a finger on the pad
//                             with no food on it)
//     CALIBRATE <grams>     — assume the current load is exactly this many
//                             grams and recompute the scale factor
//     QUIT                  — exit cleanly
//
// Errors and informational notes go to stderr prefixed with "warn: " or
// "error: " so the Go side can log them without confusing the protocol.
//

import Foundation
import OpenMultitouchSupport

// ──────────────────────────────────────────────────────────────────────────
// Calibration state
// ──────────────────────────────────────────────────────────────────────────

/// Empirical scale factor that maps the unitless `force` sum to grams.
/// This number was reverse-engineered from the TrackWeight project
/// (github.com/KrishKrosh/TrackWeight) and confirmed against a kitchen
/// scale. It is hardware-dependent — calibrate with a known weight if
/// readings are off by more than ~5%.
var gramsPerForce: Double = 100.0

/// Zero offset — the force reading observed while a finger rests on the
/// pad with no food. Captured at startup and refreshable via the `TARE`
/// stdin command. Defaults to 0 so the first few readings are still
/// usable while the user gets their finger in position.
var tareOffset: Double = 0.0

/// When > 0, we are currently averaging the next N frames into a new
/// `tareOffset`. Used by the `TARE` command.
var tareFramesRemaining: Int = 0
var tareAccumulator: Double = 0.0

/// When set, the next frame's reading is assumed to equal this many grams
/// and we recompute `gramsPerForce` from it. Used by `CALIBRATE`.
var pendingCalibrationGrams: Double? = nil

// ──────────────────────────────────────────────────────────────────────────
// stdout helpers — every line is line-buffered so Go sees readings live
// ──────────────────────────────────────────────────────────────────────────

/// Print a line and immediately flush stdout. Without flushing, macOS
/// would buffer up to 4 KB before delivering anything to the Go parent,
/// which would make the scale appear frozen.
func emit(_ line: String) {
    print(line)
    // FileHandle.standardOutput.synchronizeFile() — not needed; `print`
    // already writes to stdout, but stdout is fully-buffered when piped.
    // `setbuf(stdout, nil)` at startup (below) makes it unbuffered.
}

func warn(_ message: String) {
    FileHandle.standardError.write(Data("warn: \(message)\n".utf8))
}

// Make stdout unbuffered so each `print` line is flushed immediately.
// Without this, piping stdout to Go's `bufio.Scanner` would block until
// the OS's pipe buffer (typically 4–16 KB) filled up.
setbuf(stdout, nil)

// ──────────────────────────────────────────────────────────────────────────
// stdin command reader — runs on a background thread so we can read
// commands without blocking the multitouch event stream.
// ──────────────────────────────────────────────────────────────────────────

func startStdinReader() {
    Thread.detachNewThread {
        while let line = readLine(strippingNewline: true) {
            let parts = line.split(separator: " ", maxSplits: 1).map(String.init)
            guard let cmd = parts.first?.uppercased() else { continue }
            switch cmd {
            case "TARE":
                tareAccumulator = 0
                tareFramesRemaining = 30  // ~250 ms of averaging
            case "CALIBRATE":
                if parts.count == 2, let grams = Double(parts[1]) {
                    pendingCalibrationGrams = grams
                } else {
                    warn("CALIBRATE requires a numeric gram value")
                }
            case "QUIT":
                exit(0)
            default:
                warn("unknown command: \(cmd)")
            }
        }
    }
}

// ──────────────────────────────────────────────────────────────────────────
// Main — subscribe to touch frames, do math, print readings.
// ──────────────────────────────────────────────────────────────────────────

let manager = OMSManager.shared()
manager.startListening()

emit("READY 90")  // 90 Hz is the typical Force Touch sample rate
startStdinReader()

// `OMSManager.shared().touchDataStream` is an `AsyncStream` of arrays
// of touch contacts. We sum the force across every active contact each
// frame — both the user's finger and (indirectly, via the strain gauges)
// any object resting on the pad contribute to the total.
Task {
    for await touches in manager.touchDataStream {
        let totalForce = touches.reduce(0.0) { sum, touch in
            sum + Double(touch.total.force)
        }

        // Handle a pending CALIBRATE command: assume the current reading
        // equals the user-provided gram value, solve for `gramsPerForce`.
        if let knownGrams = pendingCalibrationGrams {
            let netForce = totalForce - tareOffset
            if abs(netForce) > 0.001 {
                gramsPerForce = knownGrams / netForce
                warn("calibrated: gramsPerForce = \(gramsPerForce)")
            } else {
                warn("calibration skipped: load too light to measure")
            }
            pendingCalibrationGrams = nil
        }

        // Handle a pending TARE command: average the next N frames'
        // force readings to set a new zero point.
        if tareFramesRemaining > 0 {
            tareAccumulator += totalForce
            tareFramesRemaining -= 1
            if tareFramesRemaining == 0 {
                tareOffset = tareAccumulator / 30.0
                warn("tare set: \(tareOffset)")
            }
        }

        // Convert force → grams, clamp negatives to zero (sometimes
        // small numerical drift produces -0.1 g).
        let grams = max(0.0, (totalForce - tareOffset) * gramsPerForce)
        emit(String(format: "READING %.2f", grams))
    }
}

// `Task` above runs asynchronously; we still need to keep the main
// thread alive so the async event loop can deliver touch frames.
RunLoop.main.run()
