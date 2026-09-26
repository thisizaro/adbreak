// Command probe runs the media stage on one file and prints shot statistics.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/thisizaro/adbreak/internal/media"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: probe <video> [threshold]")
	}
	th := 0.3
	if len(os.Args) > 2 {
		th, _ = strconv.ParseFloat(os.Args[2], 64)
	}
	ctx := context.Background()
	info, err := media.Probe(ctx, os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	cuts, err := media.DetectShots(ctx, os.Args[1], th)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%+v\nthreshold=%.2f shots=%d took=%s\n", info, th, len(cuts)+1, time.Since(start).Round(time.Millisecond))
	for i, c := range cuts {
		if i < 8 {
			fmt.Printf("  cut %.2f\n", c)
		}
	}
}
