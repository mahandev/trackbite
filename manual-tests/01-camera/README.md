# 01 — Camera + barcode smoke test

**Goal.** Confirm the webcam opens, frames are delivered, and `gozxing`
can decode a real EAN-13/UPC-A barcode using only the FaceTime HD camera.

## What it does

1. Opens camera index 0 (the built-in FaceTime camera).
2. Pulls frames in a tight loop for up to 30 seconds.
3. Attempts to decode one frame every ~166 ms (every 4th frame at
   ~24 fps).
4. Exits with code 0 and prints the decoded barcode on the first hit.
5. Exits with code 1 after 30 s if nothing was decoded.

The point isn't accuracy or speed — it's a binary "does this stack work
on this machine."

## How to run

```bash
# Build (or `make cam-test` from the repo root)
go build -tags gocv_specific_modules,gocv_videoio -o /tmp/cam-test ./manual-tests/01-camera

# Run — hold any food barcode 10-20 cm from the camera
/tmp/cam-test
```

The `-tags gocv_specific_modules,gocv_videoio` part excludes gocv's `dnn`
glue — we don't need it for barcode work, and as of OpenCV 4.13 the dnn
inline namespace was bumped past what gocv v0.43.0 was built against, so
including it triggers `Undefined symbols: cv::dnn::dnn4_v20241223...` at
link time. See findings entry below.

You can also point at a different camera with `./cam-test 1`.

## First-time gotchas

- **Camera permission.** On first run, macOS prompts to grant camera
  access. The prompt is attached to the parent process (your Terminal,
  iTerm, VS Code, etc.) — not the binary. Grant it once and you're done
  for that terminal.
- **`Library not loaded` errors.** Almost always means Homebrew has
  upgraded `protobuf` or `abseil` past what your installed `opencv` was
  built against. Fix: `brew uninstall --ignore-dependencies opencv && brew install opencv`.
  See `../findings.md`.
- **Decode fails on every frame.** Move closer (10–15 cm), brighter
  light, hold steady for a full second. EAN-13 is sensitive to motion
  blur.

## What success looks like

```
opening camera 0 ...
camera opened ✓
  ...scanning (frames=24, attempts=6)
  ...scanning (frames=48, attempts=12)

✓ DECODED: 8901719100017  (format=EAN_13)
  after 60 frames, 15 decode attempts
```
