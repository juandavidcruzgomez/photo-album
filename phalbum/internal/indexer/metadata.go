package indexer

import (
	"bytes"
	"math"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)

// metadata is what the indexer reads from a photo's EXIF block. Every field
// is optional.
type metadata struct {
	Orientation int
	TakenAt     *time.Time
	Make        string
	Model       string
	Latitude    *float64
	Longitude   *float64
}

// readMetadata decodes EXIF from the file bytes. Missing or malformed EXIF
// yields an empty result, never an error.
func readMetadata(data []byte) (m metadata) {
	defer func() {
		if recover() != nil { // goexif can panic on corrupt data
			m = metadata{}
		}
	}()
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return metadata{}
	}
	if tag, err := x.Get(exif.Orientation); err == nil {
		if o, err := tag.Int(0); err == nil {
			m.Orientation = o
		}
	}
	if t, err := x.DateTime(); err == nil && !t.IsZero() {
		m.TakenAt = &t
	}
	m.Make = stringTag(x, exif.Make)
	m.Model = stringTag(x, exif.Model)
	if lat, long, err := x.LatLong(); err == nil && validCoord(lat, long) {
		m.Latitude, m.Longitude = &lat, &long
	}
	return m
}

func stringTag(x *exif.Exif, name exif.FieldName) string {
	tag, err := x.Get(name)
	if err != nil {
		return ""
	}
	s, err := tag.StringVal()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(s, "\x00"))
}

// validCoord rejects NaNs and the 0,0 placeholder some cameras write.
func validCoord(lat, long float64) bool {
	ok := lat >= -90 && lat <= 90 && long >= -180 && long <= 180
	return ok && !(lat == 0 && long == 0) && !math.IsNaN(lat) && !math.IsNaN(long)
}
