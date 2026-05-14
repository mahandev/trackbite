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
