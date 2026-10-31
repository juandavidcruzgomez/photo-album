package embedding

import (
	"image"

	"golang.org/x/image/draw"
)

// ImageSize is the input resolution of CLIP ViT-B/32.
const ImageSize = 224

var (
	clipMean = [3]float32{0.48145466, 0.4578275, 0.40821073}
	clipStd  = [3]float32{0.26862954, 0.26130258, 0.27577711}
)

// Preprocess converts an image into a normalized CHW float32 tensor of
// 3*ImageSize*ImageSize values: resize the shortest side to ImageSize
// (bicubic), center crop, scale to [0,1] and normalize with CLIP's mean/std.
func Preprocess(img image.Image) []float32 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	var nw, nh int
	if w < h {
		nw, nh = ImageSize, max(ImageSize, h*ImageSize/w)
	} else {
		nw, nh = max(ImageSize, w*ImageSize/h), ImageSize
	}
	resized := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(resized, resized.Bounds(), img, b, draw.Src, nil)

	x0, y0 := (nw-ImageSize)/2, (nh-ImageSize)/2
	plane := ImageSize * ImageSize
	out := make([]float32, 3*plane)
	for y := 0; y < ImageSize; y++ {
		for x := 0; x < ImageSize; x++ {
			off := resized.PixOffset(x0+x, y0+y)
			i := y*ImageSize + x
			for c := 0; c < 3; c++ {
				v := float32(resized.Pix[off+c]) / 255
				out[c*plane+i] = (v - clipMean[c]) / clipStd[c]
			}
		}
	}
	return out
}
