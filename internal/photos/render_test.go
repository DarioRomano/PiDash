package photos

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"golang.org/x/image/draw"
)

var (
	red   = color.RGBA{220, 30, 30, 255}
	green = color.RGBA{30, 200, 60, 255}
	blue  = color.RGBA{30, 60, 220, 255}
	white = color.RGBA{240, 240, 240, 255}
)

// quadrants builds an image whose four corners have distinct colours:
// top-left red, top-right green, bottom-left blue, bottom-right white.
func quadrants(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := func(r image.Rectangle, c color.RGBA) {
		draw.Draw(img, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
	}
	fill(image.Rect(0, 0, w/2, h/2), red)
	fill(image.Rect(w/2, 0, w, h/2), green)
	fill(image.Rect(0, h/2, w/2, h), blue)
	fill(image.Rect(w/2, h/2, w, h), white)
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withExifOrientation inserts an APP1 EXIF segment carrying the orientation tag.
func withExifOrientation(jpg []byte, orientation uint16, bo binary.ByteOrder) []byte {
	tiff := make([]byte, 8+2+12+4)
	if bo == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	bo.PutUint16(tiff[2:], 42)
	bo.PutUint32(tiff[4:], 8)
	bo.PutUint16(tiff[8:], 1)       // one entry
	bo.PutUint16(tiff[10:], 0x0112) // Orientation
	bo.PutUint16(tiff[12:], 3)      // SHORT
	bo.PutUint32(tiff[14:], 1)
	bo.PutUint16(tiff[18:], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, payload...)
	return append(out, jpg[2:]...)
}

func decodeFrame(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != FrameW || b.Dy() != FrameH {
		t.Fatalf("frame is %dx%d, want %dx%d", b.Dx(), b.Dy(), FrameW, FrameH)
	}
	return img
}

func near(a color.Color, b color.RGBA) bool {
	r, g, bl, _ := a.RGBA()
	d := func(x uint32, y uint8) int {
		v := int(x>>8) - int(y)
		if v < 0 {
			v = -v
		}
		return v
	}
	return d(r, b.R) < 40 && d(g, b.G) < 40 && d(bl, b.B) < 40
}

func TestLandscapePhotoFillsTheFrame(t *testing.T) {
	out, err := Render(encodePNG(t, quadrants(1600, 900)), true, LayoutFill)
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFrame(t, out)
	checks := map[string]struct {
		x, y int
		want color.RGBA
	}{
		"top-left": {20, 20, red}, "top-right": {FrameW - 20, 20, green},
		"bottom-left": {20, FrameH - 20, blue}, "bottom-right": {FrameW - 20, FrameH - 20, white},
	}
	for name, c := range checks {
		if !near(f.At(c.x, c.y), c.want) {
			t.Errorf("%s = %v, want about %v", name, f.At(c.x, c.y), c.want)
		}
	}
}

func TestSlightlyDifferentAspectIsCroppedNotLetterboxed(t *testing.T) {
	// 3:2 loses under 16% when filling a 16:9 frame, so it fills the frame.
	out, err := Render(encodePNG(t, quadrants(1500, 1000)), true, LayoutFill)
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFrame(t, out)
	if !near(f.At(5, 5), red) || !near(f.At(FrameW-5, FrameH-5), white) {
		t.Errorf("corners = %v / %v, want the photo to reach the frame edges", f.At(5, 5), f.At(FrameW-5, FrameH-5))
	}
}

func TestPortraitPhotoIsShownWholeOnADarkBackdrop(t *testing.T) {
	out, err := Render(encodePNG(t, quadrants(900, 1200)), true, LayoutFill)
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFrame(t, out)
	// The photo is 810 wide, centred: x from 555 to 1365.
	if !near(f.At(560, 10), red) || !near(f.At(1360, 10), green) || !near(f.At(560, FrameH-10), blue) || !near(f.At(1360, FrameH-10), white) {
		t.Errorf("photo corners wrong: %v %v %v %v", f.At(560, 10), f.At(1360, 10), f.At(560, FrameH-10), f.At(1360, FrameH-10))
	}
	// Beside it is the darkened blur: clearly darker than the photo itself.
	r, g, b, _ := f.At(100, 100).RGBA()
	if r>>8 > 140 || g>>8 > 140 || b>>8 > 140 {
		t.Errorf("backdrop pixel is too bright: %v", f.At(100, 100))
	}
	if r>>8 < 5 && g>>8 < 5 && b>>8 < 5 {
		t.Errorf("backdrop should be a blurred copy, not black: %v", f.At(100, 100))
	}
}

func TestExifOrientationIsApplied(t *testing.T) {
	// A phone held upright stores a landscape image and tags it "rotate 90
	// clockwise" (orientation 6). Stored: 1200x900 with quadrants; upright it
	// is a 900x1200 portrait whose top-left is the stored bottom-left (blue).
	stored := encodeJPEG(t, quadrants(1200, 900))
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		data := withExifOrientation(stored, 6, bo)
		if got := exifOrientation(data); got != 6 {
			t.Fatalf("exifOrientation = %d, want 6", got)
		}
		out, err := Render(data, true, LayoutFill)
		if err != nil {
			t.Fatal(err)
		}
		f := decodeFrame(t, out)
		// Portrait: shown whole and centred, 810 wide.
		got := []color.Color{f.At(570, 20), f.At(1350, 20), f.At(570, FrameH-20), f.At(1350, FrameH-20)}
		want := []color.RGBA{blue, red, white, green} // upright TL, TR, BL, BR
		for i := range want {
			if !near(got[i], want[i]) {
				t.Errorf("corner %d = %v, want about %v", i, got[i], want[i])
			}
		}
	}

	// A source that already rotated the picture must not be rotated again.
	out, err := Render(withExifOrientation(stored, 6, binary.BigEndian), false, LayoutFill)
	if err != nil {
		t.Fatal(err)
	}
	if f := decodeFrame(t, out); !near(f.At(20, 20), red) {
		t.Errorf("applyExif=false still rotated: top-left = %v", f.At(20, 20))
	}
}

func TestAllOrientationsMapCorners(t *testing.T) {
	// For each orientation: which stored corner ends up top-left when upright.
	want := map[int]color.RGBA{1: red, 2: green, 3: white, 4: blue, 5: red, 6: blue, 7: white, 8: green}
	src := quadrants(800, 450)
	for o, c := range want {
		f := compose(src, o, LayoutFill)
		// Landscape results fill the frame; portrait results are centred.
		x := 10
		if o >= 5 {
			aspect := 450.0 / 800.0
			x = (FrameW-int(FrameH*aspect))/2 + 10
		}
		if !near(f.At(x, 10), c) {
			t.Errorf("orientation %d: top-left = %v, want about %v", o, f.At(x, 10), c)
		}
	}
}

func TestFourByThreeLandscapeFillsTheFrame(t *testing.T) {
	out, err := Render(encodePNG(t, quadrants(1200, 900)), true, LayoutFill)
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFrame(t, out)
	if !near(f.At(5, 5), red) || !near(f.At(FrameW-5, FrameH-5), white) {
		t.Errorf("a 4:3 photo should fill the frame, corners = %v / %v", f.At(5, 5), f.At(FrameW-5, FrameH-5))
	}
}

func TestBadInputIsRejected(t *testing.T) {
	if _, err := Render([]byte("<html>not an image</html>"), true, LayoutFill); err == nil {
		t.Error("expected an error for non-image data")
	}
	if exifOrientation([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF}) != 1 {
		t.Error("truncated EXIF should be treated as no orientation")
	}
	if exifOrientation(nil) != 1 {
		t.Error("empty data should be treated as no orientation")
	}
}

func TestWholeLayoutLeavesRoomToZoomWithoutCuttingThePhoto(t *testing.T) {
	// The part of the frame that is still on screen at the largest zoom.
	room := 1 - 1/float64(WholeZoom)
	left, top := int(float64(FrameW)*room/2), int(float64(FrameH)*room/2)
	right, bottom := FrameW-left, FrameH-top

	for name, size := range map[string][2]int{"widescreen": {1600, 900}, "4:3": {1600, 1200}, "portrait": {900, 1200}, "panorama": {3000, 800}} {
		out, err := Render(encodePNG(t, quadrants(size[0], size[1])), true, LayoutWhole)
		if err != nil {
			t.Fatal(err)
		}
		f := decodeFrame(t, out)

		// Find the photo: the box of pixels that have one of its four pure colours.
		minX, minY, maxX, maxY := FrameW, FrameH, -1, -1
		for y := 0; y < FrameH; y += 2 {
			for x := 0; x < FrameW; x += 2 {
				c := f.At(x, y)
				if near(c, red) || near(c, green) || near(c, blue) || near(c, white) {
					minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
				}
			}
		}
		if maxX < 0 {
			t.Fatalf("%s: no photo found in the frame", name)
		}
		if minX < left-2 || minY < top-2 || maxX > right+2 || maxY > bottom+2 {
			t.Errorf("%s: photo spans x %d..%d, y %d..%d; must stay within x %d..%d, y %d..%d", name, minX, maxX, minY, maxY, left, right, top, bottom)
		}
		// Nothing is cropped: all four corners of the photo are there, in order.
		if !near(f.At(minX+6, minY+6), red) || !near(f.At(maxX-6, minY+6), green) || !near(f.At(minX+6, maxY-6), blue) || !near(f.At(maxX-6, maxY-6), white) {
			t.Errorf("%s: the photo's corners are not all visible", name)
		}
		// It is as large as the room allows: it touches the box in one direction.
		if maxX-minX < right-left-8 && maxY-minY < bottom-top-8 {
			t.Errorf("%s: photo is %dx%d, smaller than it needs to be", name, maxX-minX, maxY-minY)
		}
		// Around it is the blurred copy, not black bars.
		r, g, b, _ := f.At(4, 4).RGBA()
		if r>>8 < 5 && g>>8 < 5 && b>>8 < 5 {
			t.Errorf("%s: the surround is black", name)
		}
	}
}

func TestIsPortraitLooksAtTheUprightShape(t *testing.T) {
	upright := encodePNG(t, quadrants(900, 1200))
	flat := encodePNG(t, quadrants(1200, 900))
	square := encodePNG(t, quadrants(1000, 1000))
	// A phone photo: stored lying flat, with a tag that says "turn me".
	turned := withExifOrientation(jpegBytes(uniform(800, 600, red)), 6, binary.BigEndian)

	for name, c := range map[string]struct {
		data    []byte
		upright bool
		want    bool
	}{
		"upright": {upright, false, true}, "flat": {flat, false, false}, "square": {square, false, false},
		"turned by its tag": {turned, false, true}, "tag already applied by the source": {turned, true, false},
	} {
		got, err := IsPortrait(c.data, c.upright)
		if err != nil || got != c.want {
			t.Errorf("%s: portrait = %v, err %v; want %v", name, got, err, c.want)
		}
	}
	if _, err := IsPortrait([]byte("not an image"), false); err == nil {
		t.Error("no error for something that is not an image")
	}
}

func TestTwoPortraitsShareTheFrame(t *testing.T) {
	first := encodePNG(t, quadrants(900, 1200))  // 3:4, close enough to half a screen to fill it
	second := encodePNG(t, quadrants(900, 1600)) // 9:16, too narrow: shown whole
	pair := NewPairRenderer(LayoutFill)
	if _, err := pair.JPEG(); err == nil {
		t.Error("an empty pair was rendered")
	}
	if err := pair.Add(first, true); err != nil {
		t.Fatal(err)
	}
	if err := pair.Add([]byte("broken"), true); err == nil {
		t.Error("broken data was accepted")
	}
	if err := pair.Add(second, true); err != nil { // the failed attempt must not have used up the place
		t.Fatal(err)
	}
	out, err := pair.JPEG()
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFrame(t, out)
	mid := FrameW / 2

	// Left: fills its half, corner to corner.
	if !near(f.At(8, 8), red) || !near(f.At(mid-12, 8), green) || !near(f.At(8, FrameH-8), blue) || !near(f.At(mid-12, FrameH-8), white) {
		t.Errorf("left photo does not fill its half: %v %v %v %v", f.At(8, 8), f.At(mid-12, 8), f.At(8, FrameH-8), f.At(mid-12, FrameH-8))
	}
	// A dark line parts the two.
	if r, g, b, _ := f.At(mid, FrameH/2).RGBA(); r>>8 > 40 || g>>8 > 40 || b>>8 > 40 {
		t.Errorf("no dark line between the photos: %v", f.At(mid, FrameH/2))
	}
	// Right: 9:16 is 607 wide, centred in its half (x 963..1920): 1138 to 1745.
	if !near(f.At(1146, 8), red) || !near(f.At(1737, 8), green) || !near(f.At(1146, FrameH-8), blue) || !near(f.At(1737, FrameH-8), white) {
		t.Errorf("right photo is not whole and centred: %v %v %v %v", f.At(1146, 8), f.At(1737, 8), f.At(1146, FrameH-8), f.At(1737, FrameH-8))
	}
	// Beside it its own blurred copy, not the neighbour's and not black.
	if c := f.At(mid+40, 40); near(c, red) {
		t.Errorf("no backdrop beside the narrow photo: %v", c)
	} else if r, g, b, _ := c.RGBA(); r>>8 < 5 && g>>8 < 5 && b>>8 < 5 {
		t.Errorf("the backdrop beside the narrow photo is black")
	}
}

func TestTwoPortraitsStayInViewWhenPhotosMove(t *testing.T) {
	pair := NewPairRenderer(LayoutWhole)
	for _, size := range [][2]int{{900, 1200}, {900, 1600}} {
		if err := pair.Add(encodePNG(t, quadrants(size[0], size[1])), true); err != nil {
			t.Fatal(err)
		}
	}
	out, err := pair.JPEG()
	if err != nil {
		t.Fatal(err)
	}
	f := decodeFrame(t, out)
	room := 1 - 1/float64(WholeZoom)
	left, top := int(float64(FrameW)*room/2), int(float64(FrameH)*room/2)
	right, bottom := FrameW-left, FrameH-top

	// Each half holds one whole photo, inside the part that stays on screen.
	for half, x0 := range []int{0, FrameW / 2} {
		minX, minY, maxX, maxY := FrameW, FrameH, -1, -1
		for y := 0; y < FrameH; y += 2 {
			for x := x0; x < x0+FrameW/2; x += 2 {
				c := f.At(x, y)
				if near(c, red) || near(c, green) || near(c, blue) || near(c, white) {
					minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
				}
			}
		}
		if maxX < 0 || minX < left-2 || minY < top-2 || maxX > right+2 || maxY > bottom+2 {
			t.Errorf("half %d: photo spans x %d..%d, y %d..%d; must stay within x %d..%d, y %d..%d", half, minX, maxX, minY, maxY, left, right, top, bottom)
			continue
		}
		if !near(f.At(minX+6, minY+6), red) || !near(f.At(maxX-6, minY+6), green) || !near(f.At(minX+6, maxY-6), blue) || !near(f.At(maxX-6, maxY-6), white) {
			t.Errorf("half %d: the photo's corners are not all visible", half)
		}
		// It does not reach into the other half.
		if (half == 0 && maxX >= FrameW/2) || (half == 1 && minX < FrameW/2) {
			t.Errorf("half %d: photo crosses the middle (x %d..%d)", half, minX, maxX)
		}
	}
}
