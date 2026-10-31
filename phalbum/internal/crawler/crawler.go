// Package crawler walks a directory tree and collects image files.
package crawler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/tiff"
)

// Image describes an image file found by Crawl.
type Image struct {
	Path      string            // absolute path
	Size      int64             // bytes
	ModTime   time.Time         // last modification time
	Extension string            // lowercase, no dot: "jpg"
	MimeType  string            // sniffed from content, e.g. "image/jpeg"
	Tags      []string          // folder names between the root and the file
	Exif      map[string]string // EXIF fields when present, else nil
}

// imageExtensions is a cheap pre-filter so we don't open every file on disk.
var imageExtensions = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true,
	"bmp": true, "tif": true, "tiff": true, "heic": true, "heif": true, "avif": true,
}

// fallbackMime covers formats that neither http.DetectContentType nor the
// system mime table reliably know.
var fallbackMime = map[string]string{
	"heic": "image/heic",
	"heif": "image/heif",
	"avif": "image/avif",
	"tif":  "image/tiff",
	"tiff": "image/tiff",
}

// errNotImage marks files whose content isn't an image despite the extension.
var errNotImage = errors.New("not an image")

// Progress is a snapshot of a running crawl, passed to the Crawl callback.
type Progress struct {
	Dir    string // directory currently being scanned
	Dirs   int    // directories entered so far
	Files  int    // regular files seen so far
	Images int    // images collected so far
	Errors int    // errors collected so far
}

// Crawl walks root recursively and returns every image found. Per-file
// errors are collected in the returned slice and never abort the walk;
// only an unusable root gives a non-nil error.
//
// If progress is non-nil it is called after every entry visited, so it
// should be cheap; callers that render output should throttle it.
func Crawl(root string, progress func(Progress)) ([]Image, []error, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s: not a directory", absRoot)
	}

	var images []Image
	var errs []error
	var p Progress

	walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) (ret error) {
		if progress != nil {
			defer func() {
				p.Images, p.Errors = len(images), len(errs)
				progress(p)
			}()
		}
		defer func() {
			if r := recover(); r != nil {
				errs = append(errs, fmt.Errorf("%s: panic: %v", path, r))
				ret = nil
			}
		}()

		if err != nil {
			errs = append(errs, err)
			if d != nil && d.IsDir() && path != absRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			p.Dir = path
			p.Dirs++
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		p.Files++
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
		if !imageExtensions[ext] {
			return nil
		}

		img, err := inspect(absRoot, path, ext)
		if errors.Is(err, errNotImage) {
			return nil
		}
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		images = append(images, img)
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	return images, errs, nil
}

// inspect opens path and builds its Image record.
func inspect(root, path, ext string) (Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return Image{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return Image{}, err
	}

	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Image{}, fmt.Errorf("%s: %w", path, err)
	}
	mimeType := detectMime(head[:n], ext)
	if !strings.HasPrefix(mimeType, "image/") {
		return Image{}, errNotImage
	}

	img := Image{
		Path:      path,
		Size:      info.Size(),
		ModTime:   info.ModTime(),
		Extension: ext,
		MimeType:  mimeType,
		Tags:      pathTags(root, path),
	}
	if _, err := f.Seek(0, io.SeekStart); err == nil {
		img.Exif = readExif(f)
	}
	return img, nil
}

func detectMime(head []byte, ext string) string {
	m := http.DetectContentType(head)
	if m != "application/octet-stream" {
		return stripParams(m)
	}
	if m := mime.TypeByExtension("." + ext); m != "" {
		return stripParams(m)
	}
	if m, ok := fallbackMime[ext]; ok {
		return m
	}
	return "application/octet-stream"
}

func stripParams(m string) string {
	if i := strings.IndexByte(m, ';'); i >= 0 {
		return strings.TrimSpace(m[:i])
	}
	return m
}

// pathTags returns the folder names between root and the file.
func pathTags(root, path string) []string {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || rel == "." {
		return []string{}
	}
	return strings.Split(rel, string(filepath.Separator))
}

// readExif returns the EXIF fields of r, or nil if there are none. Missing
// or malformed EXIF is normal (PNGs, edited files) and not an error.
func readExif(r io.Reader) map[string]string {
	x, err := exif.Decode(r)
	if err != nil {
		return nil
	}
	fields := exifFields{}
	if err := x.Walk(fields); err != nil || len(fields) == 0 {
		return nil
	}
	return fields
}

type exifFields map[string]string

func (e exifFields) Walk(name exif.FieldName, tag *tiff.Tag) error {
	e[string(name)] = strings.Trim(tag.String(), `"`)
	return nil
}

// HashFile returns the hex-encoded SHA-256 of the file's contents.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
