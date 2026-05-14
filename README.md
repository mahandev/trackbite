# trackbite

> A MacBook Air M2's trackpad weighs your food, its webcam reads the
> barcode, and a small Go program tells you how many calories you're
> about to eat — with a strong bias toward Indian brands and home‑cooked
> Indian dishes.

## Why this exists

I wanted to learn Go end‑to‑end on something more interesting than
another todo CLI. Three problems happened to overlap:

1. **Calorie tracking in India is annoying.** Most apps know what a Pop
   Tart is but not a *dal makhani*, and barcode databases skew toward
   US‑shelf brands.
2. **My MacBook already has a calibrated strain gauge under the
   trackpad.** Apple's Force Touch reports raw force in grams‑adjacent
   units. There's no reason to buy a kitchen scale.
3. **My MacBook already has a webcam.** And barcode decoders have been
   solved for two decades.

So: trackpad + webcam + a fallback chain of nutrition APIs that
prioritises Indian data. Three runtimes touch each other through
narrow, easy‑to‑inspect interfaces — exactly the surface area I wanted
to learn Go on.

## How it works

```
   ┌────────────────────────────────────────────────────────────────┐
   │                            trackbite                           │
   │                                                                │
   │   ┌─────────────┐    spawn     ┌─────────────────────────────┐ │
   │   │  Go main    │─────────────▶│  trackweighd (Swift)        │ │
   │   │             │   stdin/out  │  reads MultitouchSupport,   │ │
   │   │             │◀─grams/s─────│  prints "READING <grams>"   │ │
   │   │             │              └─────────────────────────────┘ │
   │   │             │                                              │
   │   │             │              ┌─────────────────────────────┐ │
   │   │             │──open────────▶│  FaceTime camera (gocv)    │ │
   │   │             │              │  → gozxing decodes EAN-13   │ │
   │   │             │◀─barcode─────└─────────────────────────────┘ │
   │   │             │                                              │
   │   │             │              ┌─────────────────────────────┐ │
   │   │             │──HTTP────────▶│  Open Food Facts (.in)     │ │
   │   │             │              │  USDA FoodData Central      │ │
   │   │             │              │  IFCT 2017 (embedded CSV)   │ │
   │   │             │◀─per-100g────└─────────────────────────────┘ │
   │   │             │                                              │
   │   │             │── kcal = kcal_per_100g × grams / 100         │
   │   └─────────────┘                                              │
   └────────────────────────────────────────────────────────────────┘
```

### The trackpad‑as‑scale trick

Force Touch trackpads have four strain gauges under the surface. Apple
exposes the raw force through `MultitouchSupport.framework` (a private
framework, but it works on every Apple Silicon Mac through current
macOS). The Swift helper at `trackweighd/` taps it via the
[`OpenMultitouchSupport`](https://github.com/Kyome22/OpenMultitouchSupport)
SwiftPM package, sums force across all active contacts each frame, and
prints a line of grams to stdout 30‑90× a second.

**One hardware quirk you must know:** the trackpad only reports pressure
while something *capacitive* (a finger) is touching it. A piece of food
on its own registers zero. So you keep one finger lightly on the pad
with the food next to your finger, tare the finger weight away, and the
remainder is the food. Realistic accuracy is ±0.5 g, working range
5–500 g.

### The barcode pipeline

[`gocv`](https://gocv.io/) (Go bindings for OpenCV) opens the FaceTime
camera through AVFoundation, [`gozxing`](https://github.com/makiuchi-d/gozxing)
(pure‑Go port of ZXing) decodes one frame every 250 ms. We use
`MultiFormatUPCEANReader` which covers EAN‑13, EAN‑8, UPC‑A, UPC‑E —
the formats you'll find on every consumer food package worldwide.

### The Indian‑biased nutrition chain

The app tries sources in this order and returns the first hit:

1. **Local JSON cache** (`trackbite.db`) — every successful lookup is
   persisted forever; second scans are instant and work offline.
2. **Open Food Facts** via the Indian mirror (`in.openfoodfacts.org`).
   No API key. ~80k Indian products, including Haldiram, Amul,
   Britannia, Parle, MTR, ITC, Patanjali, Mother Dairy. This is where
   nearly every branded barcode resolves.
3. **IFCT 2017** — the Indian Food Composition Tables published by NIN
   Hyderabad. Embedded into the binary as a hand‑curated CSV at
   `data/ifct_starter.csv`. Covers roti, naan, dal, idli, dosa, biryani,
   sweets, fruits, oils. Used when you type a food name instead of
   scanning.
4. **USDA FoodData Central** — generic ingredients (raw rice, oils,
   dairy) for anything IFCT doesn't have. Free API key required.

You can drop in additional sources later (Edamam, FatSecret, Nutritionix
if their free tier ever returns) — implement the three‑method `Source`
interface in `nutrition.go` and append to the chain in `main.go`.

## Project layout

```
trackbite/
├── README.md                  ← you are here
├── Makefile                   ← `make build`, `make run`, etc.
├── .env.example               ← copy to .env and fill in keys
├── go.mod / go.sum
├── main.go                    ← orchestration + main loop
├── ui.go                      ← ANSI status line + result card
├── config.go                  ← .env loader, typed Config
├── scale.go                   ← spawns trackweighd, parses stdout
├── camera.go                  ← gocv + gozxing barcode loop
├── nutrition.go               ← Source interface + Chain
├── source_openfoodfacts.go    ← Indian-mirror OFF client
├── source_usda.go             ← FDC name-search client
├── source_ifct.go             ← embedded CSV lookup
├── cache.go                   ← JSON-on-disk cache
├── util.go                    ← tiny stdlib glue
├── data/
│   └── ifct_starter.csv       ← curated IFCT 2017 + Indian brands
├── trackweighd/               ← Swift helper (SwiftPM package)
│   ├── Package.swift
│   └── Sources/trackweighd/main.swift
└── manual-tests/              ← isolation smoke tests + findings.md
    └── 01-camera/             ← gocv + gozxing standalone test
```

## Getting the API keys

You only need **one** key to use trackbite. Everything else is optional.

### 1. USDA FoodData Central — required for name fallback (free, ~30 s)

1. Go to <https://fdc.nal.usda.gov/api-key-signup>
2. Fill in name + email + reason ("personal learning project" is fine)
3. The key is returned on the response page **immediately** and emailed
4. Paste it into `.env` as `USDA_API_KEY=...`

Free tier: 1,000 requests / hour. More than enough.

### 2. Open Food Facts — no key required

The Indian mirror at `in.openfoodfacts.org` is free and unauthenticated.
We do send a polite `User-Agent` header (`trackbite/0.1 ...`) because
OFF asks all clients to identify themselves — they'll throttle/ban
anonymous clients during traffic spikes.

### 3. Optional: Edamam Food Database (free tier)

If you want a second branded‑food fallback in case Open Food Facts
misses something:

1. <https://developer.edamam.com/edamam-food-and-shopping-apis>
2. Pick "Food Database API"
3. Free tier: 10 requests/min, 10k/month
4. Paste `app_id` + `app_key` into `.env`

Wiring it in is a ~50 line file (`source_edamam.go`) that implements
the same `Source` interface — not done in v1.

### 4. Optional: FatSecret (free 10k/day, OAuth + IP whitelist)

Powerful, but their auth and IP‑whitelist requirements make it more
work than it's worth for a personal project. <https://platform.fatsecret.com/api>

### 5. Nutritionix — currently closed to public signups

As of 2026, Nutritionix has stopped accepting free trials. If you have
an existing key from before this, it still works.

## Installation

You need: macOS (Apple Silicon or Intel with Force Touch, 2015+), Go
1.21+, Xcode Command Line Tools, Homebrew.

```bash
# 1. Clone
git clone git@github.com:mahandev/trackbite.git
cd trackbite

# 2. Install macOS deps (OpenCV + pkg-config)
make deps

# 3. Configure
cp .env.example .env
# edit .env, paste your USDA_API_KEY

# 4. Build the Swift trackpad helper
make helper

# 5. Build the Go app
make app
# (gocv v0.43.0 hard-codes OpenCV 4.11's `dnn4_v20241223` namespace, but
# Homebrew now ships OpenCV 4.13 whose inline namespace is bumped to
# `dnn4_v20251223`. The Makefile builds with `-tags
# gocv_specific_modules,gocv_videoio` to exclude the dnn module — we
# don't use it. See manual-tests/findings.md.)

# 6. Run
./trackbite
```

The first run will trigger a macOS camera permission prompt attached to
your terminal — say yes.

## Calibrating the scale

The default scale factor in `trackweighd/Sources/trackweighd/main.swift`
(`gramsPerForce = 100.0`) is a starting point, not a calibrated value.
For accurate readings:

1. Run `./trackbite`
2. Put one finger lightly on the trackpad. Don't put any food on yet.
3. Type `t` and press enter. This tares (zeroes) the scale.
4. Place a known weight on the pad next to your finger — a US nickel
   (5.0 g), a Indian ₹10 coin (~7.7 g), or anything you've weighed on a
   kitchen scale.
5. Type `c <grams>` (e.g. `c 7.7`) and press enter. The helper
   recomputes its force‑to‑grams scale factor in place.

You'll need to recalibrate if you move to a different MacBook — Force
Touch hardware varies subtly between models.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `dyld: Library not loaded: libprotobuf...` or `libabsl...` | Homebrew has upgraded a transitive dep past what your installed OpenCV was built against. `brew uninstall --ignore-dependencies opencv && brew install opencv`. If the reinstall fails on a qt/qtbase symlink conflict, accept the takeover (`brew link --overwrite qtbase`). See `manual-tests/findings.md`. |
| Linker error `Undefined symbols ... cv::dnn::dnn4_v...` when building gocv | gocv v0.43.0 references an inline dnn namespace that Homebrew's current OpenCV has bumped. Build with `-tags gocv_specific_modules,gocv_videoio` to exclude dnn. The Makefile already does this. |
| `camera 0 did not open` | macOS camera permission. Open *System Settings → Privacy & Security → Camera* and enable your terminal. |
| Scale always reads 0 g | Trackpad only reports pressure when something *capacitive* (a finger) touches it. Keep one finger lightly on the pad. |
| Weight readings drift / are off | Run `t` to tare, then `c <known grams>` with a calibration weight. |
| `trackweighd binary not found` | Run `make helper`. Requires Xcode Command Line Tools (`xcode-select --install`). |
| Barcode never decodes | Hold steady 10‑15 cm from camera, better lighting, less glare on the package. |

## Why a CLI and not a GUI or web app

- **GUI (Fyne/Wails):** adds a UI framework on top of every layer —
  distracting in a learning project.
- **Web app:** browsers can't read trackpad force at all. We'd still
  need the native Swift helper, plus a JS frontend, plus a Go HTTP
  server. Three runtimes for no real gain.
- **CLI:** Go + Swift, two narrow stdin/stdout interfaces, every layer
  inspectable with `cat` and `printf`. Best learning surface.

## Credits and references

- [TrackWeight](https://github.com/KrishKrosh/TrackWeight) by Krish Patel
  — proved the trackpad‑as‑scale idea works on modern macOS.
- [`OpenMultitouchSupport`](https://github.com/Kyome22/OpenMultitouchSupport)
  by Aladdin Free Paul — Swift wrapper around MultitouchSupport.
- [`gocv`](https://gocv.io) and [`gozxing`](https://github.com/makiuchi-d/gozxing)
  — the entire camera + barcode pipeline.
- [Open Food Facts](https://world.openfoodfacts.org) — the free open
  database that makes Indian‑brand barcode lookup possible.
- IFCT 2017 — A. Longvah et al., *Indian Food Composition Tables*, NIN
  Hyderabad. The CSV in `data/` is a digitised slice of their published
  values.

## License

MIT.
