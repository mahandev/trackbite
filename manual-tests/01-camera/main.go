// cam-test — a standalone sanity check for the webcam + barcode pipeline.
//
// What it does:
//   1. Opens the FaceTime camera via gocv.
//   2. Grabs frames in a loop for up to 30 seconds.
//   3. Tries to decode an EAN/UPC barcode from each frame.
//   4. Prints progress and exits as soon as any barcode is read.
//
// Run with:
//   go run ./tools/cam-test
//
// On first run macOS will prompt for camera permission — say yes.
package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	"gocv.io/x/gocv"
)

func main() {
	idx := 0
	if len(os.Args) > 1 {
		fmt.Sscanf(os.Args[1], "%d", &idx)
	}

	fmt.Printf("opening camera %d ...\n", idx)
	cam, err := gocv.OpenVideoCapture(idx)
	if err != nil {
		log.Fatalf("open camera: %v", err)
	}
	defer cam.Close()
	if !cam.IsOpened() {
		log.Fatalf("camera %d did not open (permission denied?)", idx)
	}
	fmt.Println("camera opened ✓")

	frame := gocv.NewMat()
	defer frame.Close()

	reader := oned.NewMultiFormatUPCEANReader(nil)

	deadline := time.Now().Add(30 * time.Second)
	frames := 0
	decodeAttempts := 0
	for time.Now().Before(deadline) {
		if ok := cam.Read(&frame); !ok || frame.Empty() {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		frames++

		if frames%4 != 0 {
			// Decode at ~6 Hz (camera typically delivers ~24-30 fps).
			continue
		}
		decodeAttempts++

		img, err := frame.ToImage()
		if err != nil {
			continue
		}
		bmp, err := gozxing.NewBinaryBitmapFromImage(img)
		if err != nil {
			continue
		}
		result, err := reader.Decode(bmp, nil)
		if err == nil {
			fmt.Printf("\n✓ DECODED: %s  (format=%v)\n",
				result.GetText(), result.GetBarcodeFormat())
			fmt.Printf("  after %d frames, %d decode attempts\n",
				frames, decodeAttempts)
			return
		}
		if decodeAttempts%4 == 0 {
			fmt.Printf("  ...scanning (frames=%d, attempts=%d)\n",
				frames, decodeAttempts)
		}
	}

	fmt.Printf("\ntimed out after %d frames / %d decode attempts — no barcode found\n",
		frames, decodeAttempts)
	fmt.Println("if the camera never opened, check System Settings > Privacy & Security > Camera")
	fmt.Println("if frames were grabbed but no barcode decoded, hold a barcode steadier")
	os.Exit(1)
}
