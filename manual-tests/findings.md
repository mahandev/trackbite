# Manual Test Findings

A log of every manual test we run on the trackbite pipeline, what we learn
from each, and any environment fixes we discover along the way. New
findings get appended in chronological order — never edit old ones, just
add follow-up notes.

Each test lives in its own subdirectory under `manual-tests/` with its own
self-contained `main.go` and `README.md`.

---

## 2026-05-14 — Camera + barcode-decode smoke test (`01-camera/`)

**Why we ran it.**
Before wiring the camera into the main app we wanted three independent
confirmations:

1. `gocv` builds and links against Homebrew OpenCV on this machine
   (Apple Silicon, macOS 15).
2. The FaceTime camera actually opens and delivers frames.
3. `gozxing.NewMultiFormatUPCEANReader` can decode a real EAN-13 from
   a real frame.

Doing this in isolation means that when something later goes wrong in the
full app we know the camera stack itself is healthy.

**What we found.**

### Finding 1 — `gocv` build warnings are noise
The `go build` step emitted a wall of `ld: warning: dylib (...) was built
for newer macOS version (14.0) than being linked (13.3)`. This is harmless
— Go's CGO defaults its deployment target to 13.3 even on a 15.x machine.
No action needed. Documented here so future-us doesn't waste time chasing
it.

### Finding 2 — Homebrew breaks OpenCV by upgrading transitive deps
First run of the test binary failed at load time:

```
dyld: Library not loaded:
  /opt/homebrew/opt/protobuf/lib/libprotobuf.29.3.0.dylib
```

OpenCV 4.11.0 was bottled against protobuf 29.x, but Homebrew has since
moved `protobuf` to v34. The `.29.3.0` dylib no longer exists at the
expected path.

A second run after symlinking protobuf@29 into place surfaced the same
problem one layer deeper:

```
dyld: Library not loaded:
  /opt/homebrew/opt/abseil/lib/libabsl_log_internal_check_op.2407.0.0.dylib
```

abseil has the same issue.

**Diagnosis.** Homebrew bottles assume "always use latest" for transitive
libraries. When upstream `protobuf` / `abseil` advance their SOVERSION,
every package linked against the older SOVERSION breaks until
`brew reinstall`'d. This is a general Homebrew gotcha, not specific to
trackbite.

**Fix that actually works.** `brew reinstall opencv` — rebuilds OpenCV
against whatever versions of `protobuf` / `abseil` are currently
installed. The symlink hack works for a single missing dylib but doesn't
scale; better to do it right once.

**Fix we tried first (don't bother).** Symlinking
`libprotobuf.29.3.0.dylib → libprotobuf.29.6.0.dylib` made dyld happy
about protobuf, then abseil broke. Could keep chasing layers — but
`reinstall opencv` is faster than playing whack-a-mole.

### Finding 3 — README guidance to add
Anyone cloning this repo will hit Finding 2 the moment they `brew upgrade`
between cloning and building. The README's troubleshooting section needs
a "if you see a `Library not loaded` error mentioning protobuf or abseil,
run `brew reinstall opencv`" entry.

### Finding 4 — `brew reinstall opencv` collides with full Qt
Attempting `brew reinstall opencv` to fix Finding 2 failed too:

```
Possible conflicting files are:
/opt/homebrew/bin/qmake -> /opt/homebrew/Cellar/qt/6.8.2/bin/qmake
/opt/homebrew/bin/qt-cmake -> /opt/homebrew/Cellar/qt/6.8.2/bin/qt-cmake
... and ~12 more qt-related binaries
```

The newer opencv bottle (4.13.0_10) depends on `qtbase` (6.11), but this
machine already has the full `qt` (6.8.2) installed for something else.
Both packages want to own `/opt/homebrew/bin/qmake` etc.

**Fix options, in increasing order of disruption:**

1. **Force-link qtbase, accept it taking over qmake et al.**
   ```bash
   brew link --overwrite qtbase
   brew reinstall opencv
   ```
   Risk: anything else on this machine that relied on the full Qt's
   qmake will pick up qtbase's qmake instead. Usually fine — qmake is
   the same tool — but worth knowing.

2. **Unlink the full qt while opencv installs, relink after.**
   ```bash
   brew unlink qt
   brew reinstall opencv
   brew link --overwrite qtbase
   ```

3. **Avoid OpenCV entirely.** Swap the camera pipeline from `gocv` to
   `imagesnap` (shell out to capture a JPEG every second, decode with
   gozxing). Less elegant, but zero system-level dependencies. Maybe
   worth a future `manual-tests/02-imagesnap/` to evaluate.

**Decision left to the user.** This is a system-administration call,
not a code decision — I shouldn't unlink the full Qt for them.

### Open questions / follow-ups
- Resolve the qt/qtbase conflict, finish `brew reinstall opencv`,
  rerun the 01-camera test and confirm it decodes a real food barcode
  (e.g. a Parle-G wrapper).
- Measure decode latency once it works — should be sub-second when the
  barcode is held steady ~15 cm from the camera.
- See if `MultiFormatUPCEANReader` decodes EAN-13s that are slightly
  rotated or partially shadowed (real-world conditions).
- Smoke-test the Swift helper: `cd trackweighd && swift build -c release && ./.build/release/trackweighd`
  should print `READY 90` then a stream of `READING 0.00` lines that
  rise when you touch the trackpad.

---

## 2026-05-14 (cont.) — Resolving Findings 2 + 4 the proper way

**Why we ran it.**
We left off blocked on Finding 4 (qt/qtbase conflict) and the original
plan was to downgrade OpenCV to 4.11 so it matched the version gocv
v0.43.0 was developed against. That plan ran into a wall, and the
actual fix turned out to be simpler.

### Finding 5 — Downgrading OpenCV to 4.11 from source is blocked by an outdated Xcode CLT
- Pulled the 4.11.0_1 formula from homebrew-core's git history at
  commit `df1b11adf2d` and vendored it at `.brew/opencv-4.11.rb`.
- Dropped it into a local tap (`mahandev/local`) so `brew install
  --build-from-source` would accept it.
- `brew install --build-from-source mahandev/local/opencv` refused to
  start the compile: this machine has Xcode Command Line Tools
  **14.3.1** (from 2023), but Homebrew on macOS 26.0 demands **26.3**.
  Updating CLT means `sudo rm -rf /Library/Developer/CommandLineTools
  && sudo xcode-select --install` — a 10–15 min interactive install
  that's only worth doing if the 4.11 path is load-bearing.

We decided it wasn't (see Finding 6).

### Finding 6 — `brew uninstall opencv` auto-removed the orphaned qtbase, dissolving Finding 4
The very act of running `brew uninstall --ignore-dependencies opencv`
caused Homebrew to autoremove `qtbase/6.11.0` — because, with opencv
gone, nothing else on this machine depended on qtbase. The Finding 4
"qmake conflict between qt and qtbase" was therefore *self-healing*
once we let Homebrew clean house.

A subsequent `brew install opencv` then pulled qtbase back in fresh as
a dependency, and the `qmake` / `qt-cmake` symlinks now point at
**qtbase 6.11** instead of full qt 6.8.2. Full qt is still installed
but its CLI tools are unlinked. For trackbite this is irrelevant — we
don't touch qt at all. For anything else on this machine that needs
full qt's qmake, `brew unlink qtbase && brew link qt` would swap them
back.

### Finding 7 — gocv v0.43.0 vs OpenCV 4.13's inline `dnn4_v...` namespace
With opencv reinstalled, `go build ./manual-tests/01-camera` got past
the dyld load problem but hit a brand-new linker error:

```
Undefined symbols for architecture arm64:
  "cv::dnn::dnn4_v20241223::Net::Net()", referenced from: ...
  "cv::dnn::dnn4_v20241223::readNet(...)", referenced from: ...
```

OpenCV's `dnn` module wraps its public API in an *inline namespace*
that gets bumped any time the dnn ABI changes — currently
`cv::dnn::dnn4_v20251223` in opencv 4.13.0_10's `libopencv_dnn.dylib`.
gocv v0.43.0's C++ glue was generated against `dnn4_v20241223` (one
year older). The two don't link.

**Fix.** gocv has per-module build tags. Build with
`-tags gocv_specific_modules,gocv_videoio` and the dnn glue is
excluded entirely. The barcode pipeline only uses `VideoCapture` +
`Mat.ToImage`, so dropping dnn costs us nothing.

The `Makefile` now bakes this in for both `make app` and a new `make
cam-test` target. README troubleshooting has rows for both the dyld
case (Finding 2 / Finding 6 fix) and this linker case.

### Finding 8 — Camera permission isn't granted automatically
First post-fix run of `/tmp/cam-test`:

```
opening camera 0 ...
OpenCV: not authorized to capture video (status 0), requesting...
OpenCV: camera failed to properly initialize!
2026/05/14 15:02:36 open camera: Error opening device: 0
```

gocv loaded fine, AVFoundation responded, but macOS denied the capture
because the parent terminal hasn't been allow-listed yet under
*System Settings → Privacy & Security → Camera*. Pure
system-administration step; can't be fixed from inside the binary.
README already documents this row.

**State after this session.**
- ✓ `manual-tests/01-camera` builds against opencv 4.13.0_10 using
  selective gocv tags.
- ✓ Binary loads cleanly (no `Library not loaded` errors).
- ✓ gocv reaches AVFoundation.
- ☐ Decode test pending: needs Terminal granted Camera permission,
  then re-run `/tmp/cam-test` with a real barcode in frame.

### Open questions / follow-ups
- After granting camera permission, rerun `/tmp/cam-test` and confirm
  a real EAN-13 decodes within the 30 s budget.
- Stop trying to match OpenCV's bottled version to gocv's stated
  expectation — gocv's per-module tags are flexible enough to absorb
  the drift. Only revisit if gocv ships a new release that starts
  depending on dnn from elsewhere in the package.
- The `.brew/opencv-4.11.rb` vendored formula and the
  `mahandev/local` tap are leftover from the abandoned downgrade
  path. Both removed at end of this session.
