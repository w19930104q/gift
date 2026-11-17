package gift

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// snapshotDst renders the chain through plain Draw into a fresh destination
// allocated like proto and returns its raw pixels plus bounds.
func snapshotDst(t *testing.T, g *GIFT, src image.Image, proto draw.Image) (image.Rectangle, []byte) {
	t.Helper()
	b := g.Bounds(src.Bounds())
	dst := freshDestination(proto, b)
	g.Draw(dst, src)
	return dst.Bounds(), pixBytes(dst)
}

// pixBytes extracts the backing pixel slice for the concrete destination
// types used in this test, falling back to per-pixel RGBA capture.
func pixBytes(dst draw.Image) []byte {
	switch d := dst.(type) {
	case *image.NRGBA:
		return append([]byte(nil), d.Pix...)
	case *image.NRGBA64:
		return append([]byte(nil), d.Pix...)
	case *image.RGBA:
		return append([]byte(nil), d.Pix...)
	case *image.RGBA64:
		return append([]byte(nil), d.Pix...)
	case *image.Gray:
		return append([]byte(nil), d.Pix...)
	case *image.Gray16:
		return append([]byte(nil), d.Pix...)
	case *image.Paletted:
		return append([]byte(nil), d.Pix...)
	case genericDestination:
		return append([]byte(nil), d.NRGBA64.Pix...)
	default:
		var buf bytes.Buffer
		b := dst.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r16, g16, b16, a16 := dst.At(x, y).RGBA()
				buf.WriteByte(byte(r16 >> 8))
				buf.WriteByte(byte(g16 >> 8))
				buf.WriteByte(byte(b16 >> 8))
				buf.WriteByte(byte(a16 >> 8))
			}
		}
		return buf.Bytes()
	}
}

// equivalenceFilters is a representative subset spanning every filter family,
// including filters with internal temp images and parameter validators.
func equivalenceFilters() []Filter {
	return []Filter{
		Invert(),
		Brightness(12),
		Gamma(1.3),
		Grayscale(),
		Hue(25),
		Threshold(40),
		FlipHorizontal(),
		Rotate90(),
		Resize(5, 5, LinearResampling),
		Resize(3, 3, NearestNeighborResampling),
		GaussianBlur(0.9),
		Convolution([]float32{0, -1, 0, -1, 5, -1, 0, -1, 0}, false, true, false, 0),
		Median(3, false),
		Minimum(3, false),
		Mean(3, false),
		Pixelate(2),
		Rotate(12, color.NRGBA{0, 0, 0, 0}, LinearInterpolation),
		UnsharpMask(0.7, 1.2, 0.01),
		Sobel(),
	}
}

// TestDrawCheckedOutputIdenticalToDraw proves the validation layer changes no
// pixels: for every (source type, filter, destination type) combination the
// checked and unchecked entry points must produce byte-identical output.
func TestDrawCheckedOutputIdenticalToDraw(t *testing.T) {
	r := image.Rect(0, 0, 5, 5)
	sources := makeMatrixSources(r)
	destProtos := makeMatrixDestinations(r)
	filters := equivalenceFilters()

	for sname, src := range sources {
		for fi, f := range filters {
			g := New(f)
			outBounds := g.Bounds(src.Bounds())
			for dname, proto := range destProtos {
				wantBounds, wantPix := snapshotDst(t, g, src, proto)

				dst := freshDestination(proto, outBounds)
				if err := g.DrawChecked(dst, src); err != nil {
					t.Errorf("DrawChecked(%s/%d/%s) error: %v", sname, fi, dname, err)
					continue
				}
				gotPix := pixBytes(dst)
				if !dst.Bounds().Eq(wantBounds) {
					t.Errorf("bounds differ for %s/%d/%s: %v vs %v", sname, fi, dname, dst.Bounds(), wantBounds)
				}
				if !bytes.Equal(gotPix, wantPix) {
					t.Errorf("pixel output differs between Draw and DrawChecked: src=%s filter#%d dst=%s",
						sname, fi, dname)
				}
			}
		}
	}
}

// TestDrawAtCheckedOutputIdenticalToDrawAt covers the composition entry point.
func TestDrawAtCheckedOutputIdenticalToDrawAt(t *testing.T) {
	r := image.Rect(0, 0, 4, 4)
	src := image.NewNRGBA64(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			src.SetNRGBA64(x, y, color.NRGBA64{
				R: uint16((x + 1) * 10000), G: uint16((y + 1) * 10000),
				B: 30000, A: uint16((x + y + 1) * 8000),
			})
		}
	}

	for _, op := range []Operator{CopyOperator, OverOperator} {
		g := New(Invert(), GaussianBlur(0.6), Resize(3, 3, CubicResampling))
		big := image.Rect(0, 0, 10, 10)

		dst1 := image.NewNRGBA(big)
		g.DrawAt(dst1, src, image.Pt(2, 1), op)

		dst2 := image.NewNRGBA(big)
		if err := g.DrawAtChecked(dst2, src, image.Pt(2, 1), op); err != nil {
			t.Fatalf("DrawAtChecked op=%d: %v", op, err)
		}
		if !bytes.Equal(dst1.Pix, dst2.Pix) {
			t.Errorf("DrawAt/DrawAtChecked output differs for op=%d", op)
		}
	}
}
