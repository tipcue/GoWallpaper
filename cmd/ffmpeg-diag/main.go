package main

import (
	"flag"
	"log"
	"time"

	"github.com/tipcue/GoWallpaper/internal/video"
)

// Simple diagnostic tool to test FFmpeg video decoding in isolation
func main() {
	videoPath := flag.String("video", "assets/像素风格-房间.mp4", "path to video file")
	numFrames := flag.Int("frames", 5, "number of frames to decode")
	flag.Parse()

	log.Printf("[DIAG] Opening video: %s", *videoPath)

	// Open with timeout
	done := make(chan *video.Decoder, 1)
	var openErr error
	go func() {
		dec, err := video.Open(*videoPath, video.PixelFormatRGBA)
		if err != nil {
			openErr = err
			return
		}
		done <- dec
	}()

	select {
	case dec := <-done:
		if openErr != nil {
			log.Fatalf("[DIAG] Open failed: %v", openErr)
		}
		defer dec.Close()

		log.Printf("[DIAG] Video opened successfully: %dx%d", dec.Width(), dec.Height())
		log.Printf("[DIAG] Attempting to decode %d frames...", *numFrames)

		for i := 0; i < *numFrames; i++ {
			frameStart := time.Now()
			log.Printf("[DIAG] Reading frame %d...", i+1)

			// Try to read each frame with a timeout
			frameDone := make(chan error, 1)
			go func() {
				_, err := dec.ReadFrame()
				frameDone <- err
			}()

			select {
			case err := <-frameDone:
				if err != nil {
					log.Fatalf("[DIAG] Frame %d read failed: %v", i+1, err)
				}
				elapsed := time.Since(frameStart)
				log.Printf("[DIAG] Frame %d decoded in %v", i+1, elapsed)

			case <-time.After(10 * time.Second):
				log.Fatalf("[DIAG] Frame %d read timeout (10s)", i+1)
			}
		}

		log.Printf("[DIAG] All frames decoded successfully")

	case <-time.After(10 * time.Second):
		log.Fatal("[DIAG] Video open timeout (10s)")
	}
}
