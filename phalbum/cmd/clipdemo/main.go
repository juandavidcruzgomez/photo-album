// Command clipdemo embeds one image and several text queries with CLIP and
// prints how well each query matches the image.
//
//	go run ./cmd/clipdemo -image cat.jpg "a photo of a cat" "a photo of a dog"
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"os"
	"time"

	"github.com/juandavidcruzgomez/photo-album/phalbum/internal/embedding"
)

func main() {
	lib := flag.String("lib", "models/libonnxruntime.so.1.31.0", "path to the ONNX Runtime shared library")
	models := flag.String("models", "models/clip-vit-base-patch32", "directory with the CLIP ONNX models and tokenizer files")
	imagePath := flag.String("image", "", "image to embed")
	flag.Parse()
	queries := flag.Args()
	if *imagePath == "" || len(queries) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	f, err := os.Open(*imagePath)
	if err != nil {
		log.Fatal(err)
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		log.Fatalf("decoding %s: %v", *imagePath, err)
	}

	if err := embedding.Init(*lib); err != nil {
		log.Fatal(err)
	}
	defer embedding.Shutdown()
	clip, err := embedding.New(embedding.DefaultConfig(*models))
	if err != nil {
		log.Fatal(err)
	}
	defer clip.Close()

	start := time.Now()
	imgEmb, err := clip.EmbedImages(img)
	if err != nil {
		log.Fatal(err)
	}
	imgTime := time.Since(start)

	start = time.Now()
	textEmb, err := clip.EmbedTexts(queries...)
	if err != nil {
		log.Fatal(err)
	}
	textTime := time.Since(start)

	// CLIP's learned logit scale is 100; softmax over queries gives zero-shot probabilities.
	sims := make([]float64, len(queries))
	var total float64
	for i, e := range textEmb {
		sims[i] = float64(embedding.Cosine(imgEmb[0], e))
		total += math.Exp(100 * sims[i])
	}
	for i, q := range queries {
		fmt.Printf("%-40q cosine=%.4f  prob=%.3f\n", q, sims[i], math.Exp(100*sims[i])/total)
	}
	fmt.Printf("\nimage: %v, %d texts: %v\n", imgTime.Round(time.Millisecond), len(queries), textTime.Round(time.Millisecond))
}
