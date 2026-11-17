package gift

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
	"time"
)

// genericSource wraps an image.Image that none of the pixelGetter special
// cases matches, forcing the generic color.Color code path. It deliberately
// does not implement draw.Image.
type genericSource struct {
	inner image.Image
}

func (g genericSource) Bounds() image.Rectangle { return g.inner.Bounds() }
func (g genericSource) At(x, y int) color.Color { return g.inner.At(x, y) }
func (g genericSource) ColorModel() color.Model { return g.inner.ColorModel() }

// genericDestination is a draw.Image the pixelSetter has no special case for,
// forcing the generic NRGBA64 Set path.
type genericDestination struct {
	*image.NRGBA64
}

func makeMatrixSources(r image.Rectangle) map[string]image.Image {
	// A deterministic pattern with a gradient on every channel and varying
	// alpha, including fully transparent, fully opaque and partial pixels.
	fill := func(set func(x, y int, c color.Color)) {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				c := color.NRGBA{
					R: uint8((x*17 + y*31) & 0xff),
					G: uint8((x*53 + y*7) & 0xff),
					B: uint8((x*11 + y*67) & 0xff),
					A: uint8((x*91 + y*43) & 0xff),
				}
				set(x, y, c)
			}
		}
	}

	nrgba := image.NewNRGBA(r)
	fill(func(x, y int, c color.Color) { nrgba.Set(x, y, c) })

	nrgba64 := image.NewNRGBA64(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := nrgba.NRGBAAt(x, y)
			nrgba64.SetNRGBA64(x, y, color.NRGBA64{
				R: uint16(c.R) * 0x101, G: uint16(c.G) * 0x101,
				B: uint16(c.B) * 0x101, A: uint16(c.A) * 0x101,
			})
		}
	}

	rgba := image.NewRGBA(r)
	draw.Draw(rgba, r, nrgba, r.Min, draw.Over)
	rgba64 := image.NewRGBA64(r)
	draw.Draw(rgba64, r, nrgba64, r.Min, draw.Over)

	gray := image.NewGray(r)
	gray16 := image.NewGray16(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := nrgba.NRGBAAt(x, y)
			y8 := uint8(0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B) + 0.5)
			gray.SetGray(x, y, color.Gray{Y: y8})
			gray16.SetGray16(x, y, color.Gray16{Y: uint16(y8) * 0x101})
		}
	}

	ycbcr := image.NewYCbCr(r, image.YCbCrSubsampleRatio444)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := nrgba.NRGBAAt(x, y)
			yy, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
			i := ycbcr.YOffset(x, y)
			ycbcr.Y[i] = yy
			ycbcr.Cb[ycbcr.COffset(x, y)] = cb
			ycbcr.Cr[ycbcr.COffset(x, y)] = cr
		}
	}

	paletted := image.NewPaletted(r, color.Palette{
		color.NRGBA{0, 0, 0, 0}, color.NRGBA{255, 255, 255, 255},
		color.NRGBA{200, 100, 50, 255}, color.NRGBA{50, 100, 200, 128},
	})
	draw.Draw(paletted, r, nrgba, r.Min, draw.Over)

	return map[string]image.Image{
		"NRGBA":    nrgba,
		"NRGBA64":  nrgba64,
		"RGBA":     rgba,
		"RGBA64":   rgba64,
		"Gray":     gray,
		"Gray16":   gray16,
		"YCbCr":    ycbcr,
		"Paletted": paletted,
		"Alpha":    image.NewAlpha(r),
		"generic":  genericSource{nrgba},
	}
}

func makeMatrixDestinations(r image.Rectangle) map[string]draw.Image {
	return map[string]draw.Image{
		"NRGBA":        image.NewNRGBA(r),
		"NRGBA64":      image.NewNRGBA64(r),
		"RGBA":         image.NewRGBA(r),
		"RGBA64":       image.NewRGBA64(r),
		"Gray":         image.NewGray(r),
		"Gray16":       image.NewGray16(r),
		"Paletted":     image.NewPaletted(r, color.Palette{color.Black, color.White, color.NRGBA{1, 2, 3, 200}}),
		"generic-draw": genericDestination{image.NewNRGBA64(r)},
	}
}

// matrixFilters covers every filter family: geometric, per-channel LUT/non-LUT
// color, whole-pixel color, rank/convolution (including internal temp images),
// interpolation and the identity copy.
func matrixFilters() map[string]Filter {
	return map[string]Filter{
		"identity":    &copyimageFilter{},
		"invert":      Invert(),
		"brightness":  Brightness(15),
		"gamma":       Gamma(1.4),
		"grayscale":   Grayscale(),
		"hue":         Hue(40),
		"threshold":   Threshold(45),
		"colorfunc":   ColorFunc(func(r, g, b, a float32) (float32, float32, float32, float32) { return r, g, b, a }),
		"flip":        FlipHorizontal(),
		"rotate90":    Rotate90(),
		"crop":        Crop(image.Rect(0, 0, 2, 2)),
		"resize":      Resize(3, 3, LinearResampling),
		"nearest":     Resize(3, 3, NearestNeighborResampling),
		"gaussian":    GaussianBlur(0.8),
		"convolution": Convolution([]float32{0, 0, 0, 0, 1, 0, 0, 0, 0}, true, true, false, 0),
		"sobel":       Sobel(),
		"median":      Median(3, false),
		"minimum":     Minimum(3, false),
		"mean":        Mean(3, false),
		"pixelate":    Pixelate(2),
		"rotate":      Rotate(15, color.NRGBA{0, 0, 0, 0}, LinearInterpolation),
		"unsharp":     UnsharpMask(0.6, 1, 0),
	}
}

// TestTypeCombinationMatrix runs every (source type x filter x destination
// type) combination through both Check and DrawChecked. Nothing may panic or
// return an error: all built-in filters accept every readable source and
// writable destination. Long chains are exercised separately to make sure
// intermediate NRGBA64 images flow correctly between every filter family.
func TestTypeCombinationMatrix(t *testing.T) {
	r := image.Rect(0, 0, 4, 4)
	sources := makeMatrixSources(r)
	destinations := makeMatrixDestinations(r)
	filters := matrixFilters()

	for sname, src := range sources {
		for fname, f := range filters {
			g := New(f)
			outBounds := g.Bounds(src.Bounds())
			for dname, dstProto := range destinations {
				dst := freshDestination(dstProto, outBounds)
				if err := g.Check(src, dst); err != nil {
					t.Errorf("Check(%s -> %s -> %s) unexpected error: %v", sname, fname, dname, err)
					continue
				}
				if err := g.DrawChecked(dst, src); err != nil {
					t.Errorf("DrawChecked(%s -> %s -> %s) unexpected error: %v", sname, fname, dname, err)
					continue
				}
				assertInBoundsPixels(t, dst, sname+"->"+fname+"->"+dname)
			}
		}
	}
}

// TestLongChainAcrossAllTypes strings several filters together (including
// filters that allocate their own internal temp images) for every source and
// destination type. This is the scenario from the bug report: implicit
// conversions between alpha/16-bit/gray inputs inside a long chain.
func TestLongChainAcrossAllTypes(t *testing.T) {
	r := image.Rect(0, 0, 6, 6)
	sources := makeMatrixSources(r)
	destinations := makeMatrixDestinations(image.Rect(0, 0, 1, 1))

	chain := []Filter{
		Invert(),
		GaussianBlur(0.7),
		Grayscale(),
		Resize(8, 8, CubicResampling),
		Gamma(1.2),
		Median(3, false),
		Hue(20),
		UnsharpMask(0.5, 1, 0),
		Rotate90(),
		ColorBalance(5, -5, 10),
	}

	for sname, src := range sources {
		g := New(chain...)
		outBounds := g.Bounds(src.Bounds())
		for dname, dstProto := range destinations {
			dst := freshDestination(dstProto, outBounds)
			if err := g.Check(src, dst); err != nil {
				t.Errorf("Check long chain (%s -> %s): %v", sname, dname, err)
				continue
			}
			if err := g.DrawChecked(dst, src); err != nil {
				t.Errorf("DrawChecked long chain (%s -> %s): %v", sname, dname, err)
				continue
			}
			assertInBoundsPixels(t, dst, "long:"+sname+"->"+dname)
		}
	}
}

// freshDestination allocates a new destination of the same concrete type as
// proto with the requested bounds.
func freshDestination(proto draw.Image, bounds image.Rectangle) draw.Image {
	switch proto.(type) {
	case *image.NRGBA:
		return image.NewNRGBA(bounds)
	case *image.NRGBA64:
		return image.NewNRGBA64(bounds)
	case *image.RGBA:
		return image.NewRGBA(bounds)
	case *image.RGBA64:
		return image.NewRGBA64(bounds)
	case *image.Gray:
		return image.NewGray(bounds)
	case *image.Gray16:
		return image.NewGray16(bounds)
	case *image.Paletted:
		return image.NewPaletted(bounds, proto.(*image.Paletted).Palette)
	case genericDestination:
		return genericDestination{image.NewNRGBA64(bounds)}
	default:
		panic("unknown destination prototype")
	}
}

// assertInBoundsPixels verifies every written pixel converts back to finite
// channel values in range, catching NaN/Inf leakage and out-of-range writes.
func assertInBoundsPixels(t *testing.T, dst draw.Image, label string) {
	t.Helper()
	b := dst.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Errorf("%s: empty destination bounds %v", label, b)
		return
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r16, g16, b16, a16 := dst.At(x, y).RGBA()
			for v, ch := range map[uint32]string{r16: "r", g16: "g", b16: "b", a16: "a"} {
				if v > 0xffff {
					t.Errorf("%s: channel %s at (%d,%d) out of range: %d", label, ch, x, y, v)
				}
			}
		}
	}
}

// Test16BitDepthPreserved asserts that 16-bit input keeps its full precision
// across a filter chain whose internal storage is 16-bit NRGBA64, while an
// 8-bit source cannot exceed 8-bit effective precision.
func Test16BitDepthPreserved(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)

	// A value that is representable in 16 bits but not in 8 bits: 0x0102
	// (258). Scaled, that is 258/65535 and no 8-bit code can encode it
	// exactly (8-bit codes map to multiples of 257 in 16-bit space).
	v16 := uint16(0x0102)

	src64 := image.NewNRGBA64(r)
	src64.SetNRGBA64(0, 0, color.NRGBA64{R: v16, G: v16, B: v16, A: 0xffff})

	// A chain of identity-like geometric filters keeps values materially
	// better preserved from a 16-bit source than any 8-bit source could be.
	g := New(FlipHorizontal(), FlipVertical(), Rotate180())
	dst64 := image.NewNRGBA64(r)
	if err := g.DrawChecked(dst64, src64); err != nil {
		t.Fatalf("DrawChecked: %v", err)
	}
	got := dst64.NRGBA64At(0, 0)
	if diff := abs16(got.R, v16); diff > 1 {
		t.Errorf("16-bit value %d not preserved across chain, got R=%d (diff %d)", v16, got.R, diff)
	}
	if got.A != 0xffff {
		t.Errorf("opaque alpha not preserved: got %d", got.A)
	}

	// The same pixel fed through an 8-bit image must lose the low bits.
	src8 := image.NewNRGBA(r)
	src8.SetNRGBA(0, 0, color.NRGBA{R: uint8(v16 >> 8), G: uint8(v16 >> 8), B: uint8(v16 >> 8), A: 0xff})
	dst8 := image.NewNRGBA64(r)
	if err := g.DrawChecked(dst8, src8); err != nil {
		t.Fatalf("DrawChecked 8-bit: %v", err)
	}
	got8 := dst8.NRGBA64At(0, 0)
	// 8-bit code 1 expands to 0x0101 in 16-bit space, never 0x0102.
	if got8.R == v16 {
		t.Errorf("8-bit source unexpectedly retained 16-bit-only code %d", v16)
	}
	if abs16(got8.R, 0x0101) > 1 {
		t.Errorf("8-bit code should map near 0x0101, got %d", got8.R)
	}
}

func abs16(a, b uint16) uint16 {
	if a > b {
		return a - b
	}
	return b - a
}

// TestAlphaSemantics checks that straight alpha is carried through the chain
// and that premultiplied RGBA input is un-premultiplied to the same straight
// color as the equivalent NRGBA input.
func TestAlphaSemantics(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)

	nrgba := image.NewNRGBA(r)
	nrgba.Set(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	rgba := image.NewRGBA(r)
	rgba.Set(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 128})

	g := New(Invert(), Grayscale())
	dst1 := image.NewNRGBA64(r)
	dst2 := image.NewNRGBA64(r)
	if err := g.DrawChecked(dst1, nrgba); err != nil {
		t.Fatalf("NRGBA chain: %v", err)
	}
	if err := g.DrawChecked(dst2, rgba); err != nil {
		t.Fatalf("RGBA chain: %v", err)
	}
	c1 := dst1.NRGBA64At(0, 0)
	c2 := dst2.NRGBA64At(0, 0)
	// The premultiplied path rounds Ra=R*A/255 to a whole 8-bit code before
	// un-premultiplying, so the two representations can differ by at most
	// roughly one 8-bit code (257 in 16-bit units); this is quantization, not
	// a semantic difference.
	const premultiplyTolerance16 = uint16(300)
	if abs16(c1.R, c2.R) > premultiplyTolerance16 || abs16(c1.G, c2.G) > premultiplyTolerance16 ||
		abs16(c1.B, c2.B) > premultiplyTolerance16 || abs16(c1.A, c2.A) > premultiplyTolerance16 {
		t.Errorf("premultiplied vs straight inputs diverged beyond 8-bit quantization: NRGBA-path=%+v RGBA-path=%+v", c1, c2)
	}
	// Alpha must survive the chain unchanged at this precision.
	if want := uint16(128) * 0x101; abs16(c1.A, want) > 1 {
		t.Errorf("alpha %d not preserved, got %d", want, c1.A)
	}

	// At 16-bit depth the straight/premultiplied round trip is nearly exact:
	// the same logical color built as NRGBA64 and RGBA64 must converge.
	nrgba64 := image.NewNRGBA64(r)
	nrgba64.SetNRGBA64(0, 0, color.NRGBA64{R: 50000, G: 25000, B: 12000, A: 30000})
	rgba64 := image.NewRGBA64(r)
	rgba64.Set(0, 0, color.NRGBA64{R: 50000, G: 25000, B: 12000, A: 30000})
	dst3 := image.NewNRGBA64(r)
	dst4 := image.NewNRGBA64(r)
	if err := New(Invert(), Grayscale()).DrawChecked(dst3, nrgba64); err != nil {
		t.Fatalf("NRGBA64 chain: %v", err)
	}
	if err := New(Invert(), Grayscale()).DrawChecked(dst4, rgba64); err != nil {
		t.Fatalf("RGBA64 chain: %v", err)
	}
	c3 := dst3.NRGBA64At(0, 0)
	c4 := dst4.NRGBA64At(0, 0)
	pairs := [][2]uint16{{c3.R, c4.R}, {c3.G, c4.G}, {c3.B, c4.B}, {c3.A, c4.A}}
	for _, pair := range pairs {
		if d := abs16(pair[0], pair[1]); d > 4 {
			t.Errorf("16-bit straight/premultiplied paths diverged by %d: %+v vs %+v", d, c3, c4)
			break
		}
	}

	// Fully transparent pixels normalize to zero color with zero alpha.
	transparent := image.NewNRGBA(r)
	transparent.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 0})
	dstT := image.NewNRGBA(r)
	if err := New(Invert()).DrawChecked(dstT, transparent); err != nil {
		t.Fatalf("transparent chain: %v", err)
	}
	if got := dstT.NRGBAAt(0, 0); got.A != 0 {
		t.Errorf("transparent pixel must keep zero alpha, got %+v", got)
	}
}

// TestGraySemantics checks that gray sources are treated as opaque and that
// writing color pixels into a gray destination applies the documented
// luminance formula.
func TestGraySemantics(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)

	// Gray input is implicitly opaque: a color destination must receive
	// alpha == 255 even though Gray carries no alpha channel.
	gray := image.NewGray(r)
	gray.SetGray(0, 0, color.Gray{Y: 123})
	dstColor := image.NewNRGBA(r)
	if err := New(Invert()).DrawChecked(dstColor, gray); err != nil {
		t.Fatalf("gray->color: %v", err)
	}
	if got := dstColor.NRGBAAt(0, 0); got.A != 255 {
		t.Errorf("gray source must be treated as opaque, got alpha %d", got.A)
	}

	// Color -> Gray must use 0.299/0.587/0.114 luminance times alpha.
	src := image.NewNRGBA(r)
	src.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 50, A: 255})
	dstGray := image.NewGray(r)
	if err := New(&copyimageFilter{}).DrawChecked(dstGray, src); err != nil {
		t.Fatalf("color->gray: %v", err)
	}
	want := f32u8((0.299*200 + 0.587*100 + 0.114*50) * 1.0)
	if got := dstGray.GrayAt(0, 0).Y; abs8(got, want) > 1 {
		t.Errorf("luminance mismatch: got %d want %d", got, want)
	}

	// Gray16 round trip keeps 16-bit precision.
	gray16 := image.NewGray16(r)
	gray16.SetGray16(0, 0, color.Gray16{Y: 0x0abc})
	dstGray16 := image.NewGray16(r)
	if err := New(&copyimageFilter{}).DrawChecked(dstGray16, gray16); err != nil {
		t.Fatalf("gray16 copy: %v", err)
	}
	if got := dstGray16.Gray16At(0, 0).Y; got != 0x0abc {
		t.Errorf("gray16 value not preserved: got %#x", got)
	}
}

func abs8(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

// TestGenericPaths exercises the fallback color.Color branches end to end.
func TestGenericPaths(t *testing.T) {
	r := image.Rect(0, 0, 3, 3)
	base := image.NewNRGBA(r)
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			base.SetNRGBA(x, y, color.NRGBA{uint8(x * 80), uint8(y * 80), 128, 200})
		}
	}

	// Generic source through a filter and a generic destination.
	src := genericSource{base}
	dst := genericDestination{image.NewNRGBA64(r)}
	if err := New(Invert(), GaussianBlur(0.6)).DrawChecked(dst, src); err != nil {
		t.Fatalf("generic source/dest chain: %v", err)
	}
	assertInBoundsPixels(t, dst, "generic paths")

	// Alpha images are generic sources too; they must read without error and
	// expose their alpha through the normalized representation.
	alpha := image.NewAlpha(r)
	alpha.SetAlpha(1, 1, color.Alpha{A: 200})
	dstA := image.NewNRGBA(r)
	if err := New(&copyimageFilter{}).DrawChecked(dstA, alpha); err != nil {
		t.Fatalf("alpha source: %v", err)
	}
	if got := dstA.NRGBAAt(1, 1); got.A != 200 {
		t.Errorf("alpha source lost alpha: got %d", got.A)
	}
}

// TestUniformIsRejectedOrHandled documents the pre-existing limitation that an
// image with unbounded bounds (image.Uniform) cannot be rasterized through a
// filter chain: validation cannot see pixel bounds, so callers must pass
// finite-bounds sources. We assert Check itself stays safe (no panic, no
// infinite work) on such a source.
func TestUniformCheckIsSafe(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	src := image.NewUniform(color.NRGBA{10, 20, 30, 200})
	g := New(Invert())
	// Check only inspects types: it must return promptly without touching
	// bounds or pixels.
	done := make(chan error, 1)
	go func() { done <- g.Check(src, image.NewNRGBA(r)) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Check on Uniform source should not fail: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Check hung on an unbounded source; it must not iterate pixels")
	}
}

// TestCheckIsAllocationFree documents that validation inspects types only and
// never builds intermediate images.
func TestCheckIsAllocationFree(t *testing.T) {
	r := image.Rect(0, 0, 4, 4)
	src := image.NewNRGBA(r)
	g := New(Invert(), Resize(8, 8, LanczosResampling), Grayscale(), GaussianBlur(1))
	allocs := testing.AllocsPerRun(10, func() {
		if err := g.Check(src, image.NewNRGBA(g.Bounds(r))); err != nil {
			t.Fatalf("Check: %v", err)
		}
	})
	// reflect.TypeOf/String work dominates; the key assertion is that this
	// stays small and bounded regardless of image size.
	if allocs > 25 {
		t.Errorf("Check allocates too much (%.1f allocs); it must not build temp images", allocs)
	}
}
