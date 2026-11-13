package photos

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // decode PNG sources
	"math"

	"golang.org/x/image/draw"
)

// Frame size every background photo is rendered to. The dashboard is designed
// at this size, and the browser never has to decode anything larger.
const (
	FrameW = 1920
	FrameH = 1080
)

// maxSourcePixels guards memory: a decoded image needs about 4 bytes per pixel.
const maxSourcePixels = 80_000_000

// maxCropLoss is how much of a photo may be cut away to fill the screen. A 4:3
// landscape photo, the most common shape, loses exactly a quarter and fills the
// screen. Photos that would lose more (portraits, panoramas) are shown whole on
// a blurred, darkened copy of themselves instead.
const maxCropLoss = 0.26

// WholeZoom is how far the dashboard may zoom into a frame rendered with
// LayoutWhole before any of the photo leaves the screen. Such a frame keeps
// the photo inside its central 1/WholeZoom, in both directions.
const WholeZoom = 1.12

// Layout is how a photo is placed in its frame.
type Layout int

const (
	// LayoutFill fills the screen where little is lost by cropping, and
	// otherwise shows the whole photo on a blurred copy of itself.
	LayoutFill Layout = iota
	// LayoutWhole never crops. The photo sits whole in the middle of the
	// frame with room around it, so the dashboard can move and zoom the frame
	// slowly without any part of the photo ever leaving the screen.
	LayoutWhole
)

var errTooLarge = errors.New("image is too large to process")

// Render turns encoded image data into a FrameW x FrameH JPEG. applyExif
// rotates the image according to its EXIF orientation tag; pass false when the
// source has already rotated it (pre-made thumbnails).
func Render(data []byte, applyExif bool, layout Layout) ([]byte, error) {
	src, orientation, err := decode(data, applyExif)
	if err != nil {
		return nil, err
	}
	return encode(compose(src, orientation, layout))
}

// decode reads an image and the EXIF orientation to apply to it.
func decode(data []byte, applyExif bool) (image.Image, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("unsupported image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxSourcePixels {
		return nil, 0, errTooLarge
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("decode: %w", err)
	}
	orientation := 1
	if applyExif {
		orientation = exifOrientation(data)
	}
	return src, orientation, nil
}

func encode(frame *image.RGBA) ([]byte, error) {
	var out bytes.Buffer
	if err := jpeg.Encode(&out, frame, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// portraitAspect is the width-to-height ratio below which a photo counts as
// upright enough to share the screen with a second one.
const portraitAspect = 0.85

// IsPortrait reports whether encoded image data is an upright photo. It reads
// only the image's header. upright says the data is already rotated correctly.
func IsPortrait(data []byte, upright bool) (bool, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return false, errors.New("unsupported image")
	}
	w, h := cfg.Width, cfg.Height
	if !upright && exifOrientation(data) >= 5 {
		w, h = h, w
	}
	return float64(w)/float64(h) < portraitAspect, nil
}

// pairGap is the dark line between two photos that fill their halves, and
// pairInset how far two whole photos keep from the middle, in pixels.
const (
	pairGap   = 6
	pairInset = 10
)

// pairAreas returns, for the left and the right photo of a pair, the part of
// the frame it owns and the box inside it the photo itself must stay in.
func pairAreas(layout Layout) (areas, boxes [2]image.Rectangle) {
	mid := FrameW / 2
	if layout == LayoutWhole {
		// Both photos stay inside the part of the frame that is still on
		// screen at the largest zoom.
		c := wholeBox()
		areas = [2]image.Rectangle{image.Rect(0, 0, mid, FrameH), image.Rect(mid, 0, FrameW, FrameH)}
		boxes = [2]image.Rectangle{
			image.Rect(c.Min.X, c.Min.Y, mid-pairInset, c.Max.Y),
			image.Rect(mid+pairInset, c.Min.Y, c.Max.X, c.Max.Y),
		}
		return areas, boxes
	}
	areas = [2]image.Rectangle{image.Rect(0, 0, mid-pairGap/2, FrameH), image.Rect(mid+pairGap/2, 0, FrameW, FrameH)}
	return areas, areas
}

// wholeBox is the middle of the frame that stays on screen at WholeZoom.
func wholeBox() image.Rectangle {
	w, h := int(math.Floor(FrameW/WholeZoom)), int(math.Floor(FrameH/WholeZoom))
	return image.Rect((FrameW-w)/2, (FrameH-h)/2, (FrameW-w)/2+w, (FrameH-h)/2+h)
}

// PairRenderer builds a frame showing two upright photos side by side. The
// photos are added one after the other, so only one is in memory at a time.
type PairRenderer struct {
	frame  *image.RGBA
	layout Layout
	n      int
}

// NewPairRenderer starts an empty pair frame.
func NewPairRenderer(layout Layout) *PairRenderer {
	frame := image.NewRGBA(image.Rect(0, 0, FrameW, FrameH))
	draw.Draw(frame, frame.Bounds(), image.Black, image.Point{}, draw.Src)
	return &PairRenderer{frame: frame, layout: layout}
}

// Add places the next photo: the first on the left, the second on the right.
func (p *PairRenderer) Add(data []byte, applyExif bool) error {
	if p.n >= 2 {
		return errors.New("a pair holds two photos")
	}
	src, orientation, err := decode(data, applyExif)
	if err != nil {
		return err
	}
	areas, boxes := pairAreas(p.layout)
	place(p.frame, areas[p.n], boxes[p.n], src, orientation, p.layout == LayoutFill)
	p.n++
	return nil
}

// JPEG returns the finished frame.
func (p *PairRenderer) JPEG() ([]byte, error) {
	if p.n != 2 {
		return nil, errors.New("a pair needs two photos")
	}
	return encode(p.frame)
}

// compose scales src into a frame, applying the EXIF orientation.
func compose(src image.Image, orientation int, layout Layout) *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, FrameW, FrameH))
	box := frame.Bounds()
	if layout == LayoutWhole {
		box = wholeBox()
	}
	place(frame, frame.Bounds(), box, src, orientation, layout == LayoutFill)
	return frame
}

// place draws one photo into the part of the frame called area. With crop,
// the photo fills the area when little of it is lost that way. Otherwise the
// whole photo is shown, as large as box (a part of area) allows and centred in
// it, on a blurred, darkened copy of itself that fills the area.
func place(frame *image.RGBA, area, box image.Rectangle, src image.Image, orientation int, crop bool) {
	// Work out the layout in the photo's upright dimensions.
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()
	if orientation >= 5 { // orientations 5-8 swap width and height
		w, h = h, w
	}

	srcAspect := float64(w) / float64(h)
	areaAspect := float64(area.Dx()) / float64(area.Dy())
	loss := 1 - srcAspect/areaAspect
	if srcAspect > areaAspect {
		loss = 1 - areaAspect/srcAspect
	}

	if crop && loss <= maxCropLoss {
		// Fill the area, cropping the centre.
		cw, ch := w, int(float64(w)/areaAspect+0.5)
		if srcAspect > areaAspect {
			cw, ch = int(float64(h)*areaAspect+0.5), h
		}
		cw, ch = max(1, min(cw, w)), max(1, min(ch, h))
		rect := image.Rect((w-cw)/2, (h-ch)/2, (w-cw)/2+cw, (h-ch)/2+ch)
		scaleOriented(frame, area, src, rect, orientation)
		return
	}

	// Show the whole photo, centred, as large as its box allows.
	boxW, boxH := box.Dx(), box.Dy()
	fw, fh := boxW, int(float64(boxW)/srcAspect+0.5)
	if srcAspect < float64(boxW)/float64(boxH) {
		fw, fh = int(float64(boxH)*srcAspect+0.5), boxH
	}
	fw, fh = max(1, min(fw, boxW)), max(1, min(fh, boxH))
	fitted := image.NewRGBA(image.Rect(0, 0, fw, fh))
	scaleOriented(fitted, fitted.Bounds(), src, image.Rect(0, 0, w, h), orientation)

	// Backdrop: the photo shrunk to a few pixels and blown up again, which is a
	// cheap, smooth blur, then darkened so the photo stands out.
	tinyW, tinyH := max(8, 32*area.Dx()/FrameW), max(8, 18*area.Dy()/FrameH)
	tiny := image.NewRGBA(image.Rect(0, 0, tinyW, tinyH))
	// Cover-crop the fitted image into the tiny frame.
	tcw, tch := fw, int(float64(fw)/areaAspect+0.5)
	if tch > fh {
		tcw, tch = int(float64(fh)*areaAspect+0.5), fh
	}
	tcw, tch = max(1, tcw), max(1, tch)
	tcrop := image.Rect((fw-tcw)/2, (fh-tch)/2, (fw-tcw)/2+tcw, (fh-tch)/2+tch)
	draw.BiLinear.Scale(tiny, tiny.Bounds(), fitted, tcrop, draw.Src, nil)
	draw.CatmullRom.Scale(frame, area, tiny, tiny.Bounds(), draw.Src, nil)
	darken(frame, area, 0.42)

	offset := box.Min.Add(image.Pt((boxW-fw)/2, (boxH-fh)/2))
	draw.Draw(frame, fitted.Bounds().Add(offset), fitted, image.Point{}, draw.Src)
}

// scaleOriented scales the upright rectangle `crop` of src (given in upright
// coordinates) into dst, undoing the EXIF orientation on the way.
func scaleOriented(dst *image.RGBA, dr image.Rectangle, src image.Image, crop image.Rectangle, orientation int) {
	if orientation <= 1 || orientation > 8 {
		scale(dst, dr, src, crop.Add(src.Bounds().Min))
		return
	}
	// Scale the stored (unrotated) pixels into a buffer with the stored
	// orientation's shape, then copy it into place rotated. The buffer is at
	// most frame-sized, so this stays cheap.
	stored := uprightToStored(crop, src.Bounds().Dx(), src.Bounds().Dy(), orientation)
	bw, bh := dr.Dx(), dr.Dy()
	if orientation >= 5 {
		bw, bh = bh, bw
	}
	buf := image.NewRGBA(image.Rect(0, 0, bw, bh))
	scale(buf, buf.Bounds(), src, stored.Add(src.Bounds().Min))

	for y := 0; y < dr.Dy(); y++ {
		for x := 0; x < dr.Dx(); x++ {
			sx, sy := uprightPointToStored(x, y, dr.Dx(), dr.Dy(), orientation)
			dst.SetRGBA(dr.Min.X+x, dr.Min.Y+y, buf.RGBAAt(sx, sy))
		}
	}
}

// scale picks a kernel by how much the image shrinks: a true area-covering
// filter for big reductions, a sharper one when sizes are close.
func scale(dst *image.RGBA, dr image.Rectangle, src image.Image, sr image.Rectangle) {
	if sr.Dx() > dr.Dx()*2 {
		draw.BiLinear.Scale(dst, dr, src, sr, draw.Src, nil)
		return
	}
	draw.CatmullRom.Scale(dst, dr, src, sr, draw.Src, nil)
}

// uprightPointToStored maps a pixel of the upright w x h image to the pixel of
// the stored image it comes from, for EXIF orientations 2-8.
func uprightPointToStored(x, y, w, h, orientation int) (int, int) {
	switch orientation {
	case 2: // mirrored horizontally
		return w - 1 - x, y
	case 3: // rotated 180
		return w - 1 - x, h - 1 - y
	case 4: // mirrored vertically
		return x, h - 1 - y
	case 5: // transposed
		return y, x
	case 6: // stored image must be rotated 90 clockwise to be upright
		return y, w - 1 - x
	case 7: // transversed
		return h - 1 - y, w - 1 - x
	case 8: // stored image must be rotated 90 counter-clockwise to be upright
		return h - 1 - y, x
	}
	return x, y
}

// uprightToStored maps a rectangle in upright coordinates to stored coordinates.
// sw and sh are the stored image's width and height.
func uprightToStored(r image.Rectangle, sw, sh, orientation int) image.Rectangle {
	w, h := sw, sh
	if orientation >= 5 {
		w, h = sh, sw // upright size
	}
	x0, y0 := uprightPointToStored(r.Min.X, r.Min.Y, w, h, orientation)
	x1, y1 := uprightPointToStored(r.Max.X-1, r.Max.Y-1, w, h, orientation)
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	return image.Rect(x0, y0, x1+1, y1+1)
}

// darken dims the part r of img.
func darken(img *image.RGBA, r image.Rectangle, factor float64) {
	f := uint32(factor * 256)
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.Pix[img.PixOffset(r.Min.X, y) : img.PixOffset(r.Max.X-1, y)+4]
		for i := 0; i+3 < len(row); i += 4 {
			row[i] = uint8(uint32(row[i]) * f >> 8)
			row[i+1] = uint8(uint32(row[i+1]) * f >> 8)
			row[i+2] = uint8(uint32(row[i+2]) * f >> 8)
			row[i+3] = 0xff
		}
	}
}

// exifOrientation reads the EXIF orientation tag (1-8) from JPEG data and
// returns 1 when there is none. Cameras and phones store photos unrotated and
// record how to turn them here.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data starts; no EXIF before it
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off:]))
	for e := 0; e < n; e++ {
		p := off + 2 + e*12
		if p+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[p:]) == 0x0112 { // Orientation
			v := int(bo.Uint16(t[p+8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
