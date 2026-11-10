// Package crawler walks a directory tree and collects image files.
package crawler

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
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

// errTooSmall marks images smaller than Options.MinSize: icons and thumbnails.
var errTooSmall = errors.New("too small")

// skipDirs are folders that only hold caches, thumbnails or deleted files,
// written by NAS systems, macOS and Windows. Hidden folders are skipped too.
var skipDirs = map[string]bool{
	"@eaDir": true, "@__thumb": true, "#recycle": true, "#snapshot": true,
	"__MACOSX": true, "$RECYCLE.BIN": true, "System Volume Information": true,
}

// Options control a crawl.
type Options struct {
	// BatchSize is how many images are passed to the handler at a time.
	BatchSize int
	// MinSize skips images whose longest side is shorter, in pixels, such as
	// icons and thumbnails. 0 keeps every size. Images whose size can't be read
	// from their header, such as HEIC, are kept.
	MinSize int
}

// Progress is a snapshot of a running crawl, passed to the Crawl callback.
type Progress struct {
	Dir    string // directory currently being scanned
	Dirs   int    // directories entered so far
	Files  int    // regular files seen so far
	Images int    // images collected so far
	Small  int    // images skipped as smaller than Options.MinSize
	Errors int    // errors collected so far
}

// Crawl walks root recursively and passes the images it finds to handle in
// batches of up to opts.BatchSize, as it goes. Hidden files and folders, and
// the cache folders in skipDirs, are not visited. The walk waits while handle runs, and
// images are only held until their batch is handled. Per-file errors are
// collected in the returned slice and never abort the walk; only an unusable
// root gives a non-nil error.
//
// If progress is non-nil it is called after every entry visited, so it
// should be cheap; callers that render output should throttle it.
func Crawl(root string, opts Options, handle func([]Image), progress func(Progress)) ([]error, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s: not a directory", absRoot)
	}

	var batch []Image
	var errs []error
	var p Progress

	walkErr := filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) (ret error) {
		if progress != nil {
			defer func() {
				p.Errors = len(errs)
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
		if path != absRoot && (strings.HasPrefix(d.Name(), ".") || (d.IsDir() && skipDirs[d.Name()])) {
			if d.IsDir() {
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

		img, err := inspect(absRoot, path, ext, opts.MinSize)
		if errors.Is(err, errNotImage) {
			return nil
		}
		if errors.Is(err, errTooSmall) {
			p.Small++
			return nil
		}
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		p.Images++
		batch = append(batch, img)
		if len(batch) >= opts.BatchSize {
			full := batch
			batch = nil
			handle(full)
		}
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}
	if len(batch) > 0 {
		handle(batch)
	}
	return errs, nil
}

// inspect opens path and builds its Image record.
func inspect(root, path, ext string, minSize int) (Image, error) {
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
	if minSize > 0 {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return Image{}, fmt.Errorf("%s: %w", path, err)
		}
		cfg, _, err := image.DecodeConfig(bufio.NewReader(f))
		if err == nil && max(cfg.Width, cfg.Height) < minSize {
			return Image{}, errTooSmall
		}
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
