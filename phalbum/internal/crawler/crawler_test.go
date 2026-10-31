package crawler

import (
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFile(t *testing.T, path string, write func(*os.File) error) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := write(f); err != nil {
		t.Fatal(err)
	}
}

func TestCrawl(t *testing.T) {
	root := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))

	writeFile(t, filepath.Join(root, "a", "b", "x.png"), func(f *os.File) error { return png.Encode(f, img) })
	writeFile(t, filepath.Join(root, "y.JPG"), func(f *os.File) error { return jpeg.Encode(f, img, nil) })
	writeFile(t, filepath.Join(root, "fake.jpg"), func(f *os.File) error { _, err := f.WriteString("not an image"); return err })
	writeFile(t, filepath.Join(root, "notes.txt"), func(f *os.File) error { _, err := f.WriteString("hello"); return err })

	locked := filepath.Join(root, "locked")
	writeFile(t, filepath.Join(locked, "z.png"), func(f *os.File) error { return png.Encode(f, img) })
	lockable := os.Geteuid() != 0
	if lockable {
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
	}

	var last Progress
	images, errs, err := Crawl(root, func(p Progress) { last = p })
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]Image{}
	for _, im := range images {
		got[filepath.Base(im.Path)] = im
	}

	x, ok := got["x.png"]
	if !ok {
		t.Fatalf("x.png not found in %v", images)
	}
	if x.MimeType != "image/png" || x.Extension != "png" || x.Size == 0 || x.Exif != nil {
		t.Errorf("x.png: unexpected %+v", x)
	}
	if !reflect.DeepEqual(x.Tags, []string{"a", "b"}) {
		t.Errorf("x.png tags = %v, want [a b]", x.Tags)
	}
	if x.ModTime.IsZero() {
		t.Error("x.png: ModTime not set")
	}
	if !filepath.IsAbs(x.Path) {
		t.Errorf("x.png path not absolute: %s", x.Path)
	}

	y, ok := got["y.JPG"]
	if !ok {
		t.Fatalf("y.JPG not found in %v", images)
	}
	if y.MimeType != "image/jpeg" || y.Extension != "jpg" || len(y.Tags) != 0 {
		t.Errorf("y.JPG: unexpected %+v", y)
	}

	if _, ok := got["fake.jpg"]; ok {
		t.Error("fake.jpg should be skipped")
	}
	if _, ok := got["notes.txt"]; ok {
		t.Error("notes.txt should be ignored")
	}

	if last.Images != len(images) || last.Errors != len(errs) || last.Files < 4 || last.Dirs < 3 {
		t.Errorf("unexpected final progress %+v", last)
	}

	if lockable {
		if len(errs) == 0 {
			t.Error("expected an error for the unreadable directory")
		}
		if len(images) != 2 {
			t.Errorf("got %d images, want 2", len(images))
		}
	}
}

func TestCrawlMissingRoot(t *testing.T) {
	if _, _, err := Crawl(filepath.Join(t.TempDir(), "nope"), nil); err == nil {
		t.Error("expected error for missing root")
	}
}

func TestHashFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Errorf("HashFile = %s, want %s", got, want)
	}
}
