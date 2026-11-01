// Package imaging decodes photos and prepares the derived images the indexer
// needs: an upright working copy and a JPEG thumbnail.
package imaging

import (
	"bytes"
	"image"
	"image/jpeg"

	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Shrink scales img down so its shorter side is at most minSide, keeping the
// aspect ratio. Smaller images are only converted to RGBA. Working on a
// shrunk copy keeps orientation and thumbnailing cheap for large photos.
func Shrink(img image.Image, minSide int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if short := min(w, h); short > minSide {
		w, h = w*minSide/short, h*minSide/short
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// Orient returns img turned upright according to its EXIF orientation
// (1–8). Unknown values return img unchanged.
func Orient(img *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2: // mirrored horizontally
				dx, dy = w-1-x, y
			case 3: // rotated 180°
				dx, dy = w-1-x, h-1-y
			case 4: // mirrored vertically
				dx, dy = x, h-1-y
			case 5: // transposed
				dx, dy = y, x
			case 6: // needs 90° clockwise
				dx, dy = h-1-y, x
			case 7: // transversed
				dx, dy = h-1-y, w-1-x
			case 8: // needs 90° counter-clockwise
				dx, dy = y, w-1-x
			}
			dst.SetRGBA(dx, dy, img.RGBAAt(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// Thumbnail scales img to fit in maxSide×maxSide and encodes it as JPEG.
func Thumbnail(img image.Image, maxSide int) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if long := max(w, h); long > maxSide {
		w, h = max(w*maxSide/long, 1), max(h*maxSide/long, 1)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
