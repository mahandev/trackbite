package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	"gocv.io/x/gocv"
)

// Camera wraps the webcam capture + barcode decode pipeline.
//
// Pipeline:
//   1. gocv.OpenVideoCapture(idx)        — open the webcam via AVFoundation
//   2. cam.Read(&frame)                  — pull the latest frame as a Mat
//   3. frame.ToImage()                   — convert to a Go image.Image
//   4. gozxing.NewBinaryBitmapFromImage  — wrap in ZXing's bitmap type
//   5. reader.Decode(...)                — try every supported 1-D format
//
// We only decode every ~250 ms instead of every frame. Decoding is CPU-
// heavy and we don't need 30 attempts/second to find a barcode held
// steady in front of the camera.
//
// Most frames will fail to decode (the camera is mostly looking at empty
// space) — that's normal. We only report success.
type Camera struct {
	dev     *gocv.VideoCapture
	reader  gozxing.Reader
	last    string
	mu      sync.RWMutex
	stopped chan struct{}
}

// StartCamera opens webcam `index` (0 = built-in FaceTime camera on a
// MacBook) and starts a background decoder goroutine. Detected barcodes
// are buffered on `out`; the channel is closed when the camera is shut
// down or the context is cancelled.
//
// macOS camera permission: the first time this runs, you'll see a TCC
// prompt attached to your Terminal (or VS Code, or whichever process
// launched the binary). Grant once and you're set.
func StartCamera(ctx context.Context, index int, out chan<- string) (*Camera, error) {
	dev, err := gocv.OpenVideoCapture(index)
	if err != nil {
		return nil, fmt.Errorf("open webcam %d: %w", index, err)
	}
	if !dev.IsOpened() {
		return nil, fmt.Errorf("webcam %d did not open (camera permission denied?)", index)
	}

	c := &Camera{
		dev: dev,
		// MultiFormatUPCEANReader covers EAN-13, EAN-8, UPC-A, UPC-E —
		// every 1-D format you'll see on consumer food packaging
		// worldwide. Indian retail uses EAN-13 almost exclusively. We
		// pass nil hints to keep the reader flexible; tuning hints can
		// speed it up later.
		reader:  oned.NewMultiFormatUPCEANReader(nil),
		stopped: make(chan struct{}),
	}
	go c.loop(ctx, out)
	return c, nil
}

func (c *Camera) loop(ctx context.Context, out chan<- string) {
	defer close(c.stopped)
	defer close(out)

	frame := gocv.NewMat()
	defer frame.Close()

	// Decode at ~4 Hz. Barcode scanning doesn't benefit from faster
	// decoding — the user needs time to hold the package steady anyway.
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if ok := c.dev.Read(&frame); !ok || frame.Empty() {
			continue
		}

		img, err := frame.ToImage()
		if err != nil {
			continue
		}
		code, err := c.decode(img)
		if err != nil {
			// Most frames will not contain a readable barcode. That's
			// expected, not an error worth surfacing.
			continue
		}

		c.mu.Lock()
		isNew := code != c.last
		c.last = code
		c.mu.Unlock()
		if !isNew {
			// Already announced this barcode; don't spam the channel.
			continue
		}
		select {
		case out <- code:
		case <-ctx.Done():
			return
		}
	}
}

func (c *Camera) decode(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	result, err := c.reader.Decode(bmp, nil)
	if err != nil {
		return "", err
	}
	text := result.GetText()
	if text == "" {
		return "", errors.New("empty barcode")
	}
	return text, nil
}

// LastSeen returns the most recently decoded barcode (empty if none yet).
// Thread-safe.
func (c *Camera) LastSeen() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.last
}

// Reset clears the "already announced" memory so the same barcode can
// be reported again — useful after the user weighs one item and wants
// to scan another of the same SKU.
func (c *Camera) Reset() {
	c.mu.Lock()
	c.last = ""
	c.mu.Unlock()
}

// Close releases the webcam handle and waits for the decode loop to
// exit. Safe to call multiple times.
func (c *Camera) Close() error {
	if c.dev != nil {
		c.dev.Close()
		c.dev = nil
	}
	<-c.stopped
	return nil
}
