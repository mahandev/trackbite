# 02 — Trackpad scale smoke test

**Goal.** Confirm the Swift `trackweighd` helper actually reports pressure
from the Force Touch trackpad, independent of the main app's smoothing,
UI, camera, and nutrition layers.

If raw `READING` values move here but the main app shows 0 g, the bug is
above the helper (Go side: smoothing, UI). If readings don't move here,
the problem is in the helper, in OS permissions, or — most often — in the
hardware quirk described below.

## The one critical hardware quirk

Apple's Force Touch trackpads only report pressure while a **capacitive
contact** (a finger) is touching the pad. A dry, non-conductive object on
its own registers exactly zero. To weigh food:

1. Rest one finger lightly on the trackpad.
2. Put the food **next to your finger** (not on top of it).
3. The strain gauges measure the combined weight; the helper subtracts a
   tared offset captured while only your finger was touching.

If you put food on the trackpad without a finger on the pad, READING will
stay at 0.00. That's hardware, not a bug.

## How to run

```bash
# 1. Make sure the helper is built (idempotent).
make helper

# 2. Build this test.
go build -o /tmp/scale-test ./manual-tests/02-scale

# 3. Run it from the repo root so the default helper path resolves.
/tmp/scale-test
```

Or point at a helper elsewhere:

```bash
/tmp/scale-test /absolute/path/to/trackweighd
```

## What the output looks like

```
─── trackweighd smoke test ───
helper: trackweighd/bin/trackweighd

(banner with walkthrough...)

Streaming raw helper output...

[  0.01s] READY 90
[  0.02s] READING 0.00
[  0.03s] READING 0.00
...
[  4.12s] READING 47.31      ← finger lands on pad
[  4.13s] READING 51.20
...
>> sent TARE — keep one finger on the pad for ~250 ms
warn: tare set: 0.4731
[  6.40s] READING 0.21
[  6.41s] READING 0.18       ← tared; finger weight subtracted
...
[  8.55s] READING 38.42      ← placed a 40 g object next to finger
[  8.56s] READING 40.10
```

## Walk this sequence

1. **Hands off the laptop.** Watch for `READING 0.00`. Anything else here
   means there's a stray contact or the tare is bad — restart the test.
2. **Rest one finger lightly on the pad.** READING should jump to
   roughly 30–80 (your finger's contact force varies). It will not stay
   perfectly still — Force Touch is noisy by design.
3. **Type `t` + enter.** Wait ~250 ms. Keep the finger still during this
   window. After the helper prints `warn: tare set: ...`, readings should
   fall back to near 0.
4. **With the finger still on the pad, slide a small known weight next
   to it** (a coin, a packet, anything). READING should rise by something
   in the right ballpark. The default calibration is approximate; use
   `c <grams>` to dial it in.
5. **Lift your finger but leave the object.** READING collapses to 0.
   That's the hardware quirk in action — confirms the helper is wired
   correctly to the strain gauges.

## If readings never move

In rough order of likelihood:

- **No finger on the pad.** See the quirk section above. This is by far
  the most common cause.
- **Trackpad isn't a Force Touch model.** Pre-2015 MacBooks, external
  trackpads, and the Magic Trackpad 1 don't have strain gauges.
- **Input Monitoring / Accessibility permission denied.** Some macOS
  versions require the parent terminal to be allowed under *System
  Settings → Privacy & Security → Input Monitoring*. The helper doesn't
  prompt — if it's broken on a Force Touch machine and you've definitely
  got a finger on the pad, this is the next thing to check.
- **`trackweighd` binary not actually rebuilt.** Re-run `make helper`
  and watch for a non-zero recompile.

## Commands

| Input         | What happens                                                              |
| ------------- | ------------------------------------------------------------------------- |
| `t`           | Send `TARE` — averages the next ~30 frames as the new zero offset.        |
| `c <grams>`   | Send `CALIBRATE <grams>` — sets `gramsPerForce` from current load.        |
| `v`           | Send `VERBOSE` — toggles per-touch dumps on the helper's stderr (~5 Hz).  |
| `q`           | Send `QUIT` and exit.                                                     |
| Ctrl-C        | Same as `q` (SIGINT is forwarded to the helper through context).          |

## Verbose mode — reading the per-touch dump

When you toggle `v`, every ~18th frame the helper emits something like:

```
v: touches=1 totalForce=0.4862
v:   id=12 state=touching total=0.4862 pressure=0.3201 pos=(0.512,0.481)
```

What to look for as you place an object next to a resting finger:

- **`touches` stays at 1, `total` rises** → the OS is attributing the
  object's weight to your finger contact. The pipeline is working; if
  the main app still shows 0 g, the issue is downstream (smoothing,
  tare offset, calibration).
- **`touches` stays at 1, `total` doesn't change** → the OS is *not*
  propagating the object's deflection to the finger's `total`. Pure
  "finger next to food" doesn't work on this hardware. Try resting
  the finger so the food's weight transfers *through* the finger
  (e.g. food on top of a flat-laid finger).
- **`touches` drops to 0** → macOS dropped the contact entirely. That's
  the rest-rejection case; wiggling the finger should bring it back.
- **`state` flips to `lingering` and `total` drops** → softer form of
  the same thing; OS reclassified the contact as resting.
