package photos

import (
	"encoding/binary"
	"math"
	"time"
)

// Extra is what a photo file says about itself beyond its picture.
type Extra struct {
	// Taken is when the camera's clock said the photo was made, written down
	// as UTC; zero when the file does not say.
	Taken time.Time
	// Located says that Latitude and Longitude are set.
	Located             bool
	Latitude, Longitude float64
}

// exifSegment returns the TIFF data of a JPEG's EXIF block, or nil.
func exifSegment(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return nil
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data starts; no EXIF before it
			return nil
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return nil
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return seg[6:]
		}
		i += 2 + size
	}
	return nil
}

// tiff reads the directories of an EXIF block.
type tiff struct {
	data []byte
	bo   binary.ByteOrder
}

type tiffEntry struct {
	tag, kind uint16
	count     uint32
	// value is the four bytes that hold the value itself or where it is.
	value []byte
}

func openTIFF(t []byte) (tiff, int, bool) {
	if len(t) < 8 {
		return tiff{}, 0, false
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return tiff{}, 0, false
	}
	return tiff{t, bo}, int(bo.Uint32(t[4:])), true
}

// entries lists one directory; nothing when it does not fit in the data.
func (t tiff) entries(off int) []tiffEntry {
	if off < 8 || off+2 > len(t.data) {
		return nil
	}
	n := int(t.bo.Uint16(t.data[off:]))
	var out []tiffEntry
	for e := 0; e < n; e++ {
		p := off + 2 + e*12
		if p+12 > len(t.data) {
			break
		}
		out = append(out, tiffEntry{
			tag: t.bo.Uint16(t.data[p:]), kind: t.bo.Uint16(t.data[p+2:]),
			count: t.bo.Uint32(t.data[p+4:]), value: t.data[p+8 : p+12],
		})
	}
	return out
}

// bytes returns what an entry of single bytes or text holds.
func (t tiff) bytes(e tiffEntry) []byte {
	if e.count <= 4 {
		return e.value[:e.count]
	}
	off := int(t.bo.Uint32(e.value))
	if off < 0 || e.count > 1<<16 || off+int(e.count) > len(t.data) {
		return nil
	}
	return t.data[off : off+int(e.count)]
}

// rationals returns the fractions of an entry, as numbers.
func (t tiff) rationals(e tiffEntry) []float64 {
	if e.kind != 5 || e.count == 0 || e.count > 8 {
		return nil
	}
	off := int(t.bo.Uint32(e.value))
	if off < 0 || off+int(e.count)*8 > len(t.data) {
		return nil
	}
	out := make([]float64, e.count)
	for i := range out {
		num, den := t.bo.Uint32(t.data[off+i*8:]), t.bo.Uint32(t.data[off+i*8+4:])
		if den == 0 {
			return nil
		}
		out[i] = float64(num) / float64(den)
	}
	return out
}

// readExtra reads the time and the place out of a JPEG's EXIF block. The
// start of the file is enough: cameras put the block first.
func readExtra(data []byte) Extra {
	var out Extra
	t, first, ok := openTIFF(exifSegment(data))
	if !ok {
		return out
	}
	for _, e := range t.entries(first) {
		switch e.tag {
		case 0x8769: // the camera's own directory: when
			for _, x := range t.entries(int(t.bo.Uint32(e.value))) {
				if x.tag == 0x9003 && x.kind == 2 { // DateTimeOriginal
					text := string(t.bytes(x))
					if len(text) >= 19 {
						if when, err := time.Parse("2006:01:02 15:04:05", text[:19]); err == nil && when.Year() > 1970 {
							out.Taken = when
						}
					}
				}
			}
		case 0x8825: // the position directory: where
			var lat, lon []float64
			south, west := false, false
			for _, g := range t.entries(int(t.bo.Uint32(e.value))) {
				switch g.tag {
				case 1:
					south = len(t.bytes(g)) > 0 && t.bytes(g)[0] == 'S'
				case 2:
					lat = t.rationals(g)
				case 3:
					west = len(t.bytes(g)) > 0 && t.bytes(g)[0] == 'W'
				case 4:
					lon = t.rationals(g)
				}
			}
			degrees := func(v []float64, negative bool) (float64, bool) {
				if len(v) != 3 {
					return 0, false
				}
				d := v[0] + v[1]/60 + v[2]/3600
				if negative {
					d = -d
				}
				return d, !math.IsNaN(d)
			}
			la, okLa := degrees(lat, south)
			lo, okLo := degrees(lon, west)
			// A camera without a fix writes zeros.
			if okLa && okLo && math.Abs(la) <= 90 && math.Abs(lo) <= 180 && !(la == 0 && lo == 0) {
				out.Located, out.Latitude, out.Longitude = true, la, lo
			}
		}
	}
	return out
}
