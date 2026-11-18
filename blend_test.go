package gift

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"
)

// blendTestModes lists all supported blend modes.
var blendTestModes = []BlendMode{
	Normal, Multiply, Screen, Overlay, Darken, Lighten,
	ColorDodge, ColorBurn, HardLight, SoftLight,
	Difference, Exclusion, Add, Subtract,
}

// blendTestBackdrop and blendTestSource are the fixed layer matrix (4x3
// pixels each) used for the blend acceptance tests. The matrix covers
// opaque, semi-transparent and fully transparent pixels, including
// transparent pixels carrying non-zero "hidden" colors.
var blendTestBackdrop = []color.NRGBA{
	{0, 0, 0, 255}, {255, 255, 255, 255}, {128, 64, 192, 255}, {10, 20, 30, 255},
	{255, 0, 0, 128}, {0, 255, 0, 128}, {0, 0, 255, 128}, {77, 88, 99, 200},
	{0, 0, 0, 0}, {255, 255, 255, 0}, {100, 150, 200, 0}, {50, 60, 70, 1},
}

var blendTestSource = []color.NRGBA{
	{255, 255, 255, 255}, {0, 0, 0, 255}, {64, 192, 128, 255}, {200, 100, 50, 255},
	{0, 255, 255, 128}, {255, 0, 255, 128}, {255, 255, 0, 128}, {33, 66, 99, 100},
	{0, 0, 0, 0}, {255, 0, 0, 0}, {20, 40, 60, 0}, {250, 240, 230, 255},
}

func nrgbaImage(w, h int, px []color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i, c := range px {
		img.Pix[i*4+0] = c.R
		img.Pix[i*4+1] = c.G
		img.Pix[i*4+2] = c.B
		img.Pix[i*4+3] = c.A
	}
	return img
}

// refBlendChannel is an independent reference implementation of the blend
// mode functions, written directly from the W3C "Compositing and Blending
// Level 1" specification. It operates on straight (non-premultiplied)
// color values in the range [0, 1].
func refBlendChannel(mode BlendMode, cb, cs float64) float64 {
	hardLight := func(cb, cs float64) float64 {
		if cs <= 0.5 {
			return 2 * cb * cs
		}
		return 1 - 2*(1-cb)*(1-cs)
	}
	switch mode {
	case Multiply:
		return cb * cs
	case Screen:
		return cb + cs - cb*cs
	case Overlay:
		return hardLight(cs, cb)
	case Darken:
		return math.Min(cb, cs)
	case Lighten:
		return math.Max(cb, cs)
	case ColorDodge:
		if cb == 0 {
			return 0
		}
		if cs == 1 {
			return 1
		}
		return math.Min(1, cb/(1-cs))
	case ColorBurn:
		if cb == 1 {
			return 1
		}
		if cs == 0 {
			return 0
		}
		return 1 - math.Min(1, (1-cb)/cs)
	case HardLight:
		return hardLight(cb, cs)
	case SoftLight:
		if cs <= 0.5 {
			return cb - (1-2*cs)*cb*(1-cb)
		}
		var d float64
		if cb <= 0.25 {
			d = ((16*cb-12)*cb + 4) * cb
		} else {
			d = math.Sqrt(cb)
		}
		return cb + (2*cs-1)*(d-cb)
	case Difference:
		return math.Abs(cb - cs)
	case Exclusion:
		return cb + cs - 2*cb*cs
	case Add:
		return math.Min(1, cb+cs)
	case Subtract:
		return math.Max(0, cb-cs)
	default: // Normal
		return cs
	}
}

// refBlendPixel composites the source pixel ps over the backdrop pixel pb
// using the source-over compositing formula from the W3C specification.
// Pixels are straight (non-premultiplied) RGBA values in the range [0, 1].
func refBlendPixel(mode BlendMode, pb, ps [4]float64) [4]float64 {
	as := ps[3]
	ab := pb[3]
	ao := as + ab*(1-as)
	if ao <= 0 {
		return [4]float64{}
	}
	var out [4]float64
	for c := 0; c < 3; c++ {
		cb := pb[c]
		cs := ps[c]
		out[c] = (as*cs*(1-ab) + ab*cb*(1-as) + as*ab*refBlendChannel(mode, cb, cs)) / ao
	}
	out[3] = ao
	return out
}

func nrgbaToFloats(c color.NRGBA) [4]float64 {
	return [4]float64{
		float64(c.R) / 255,
		float64(c.G) / 255,
		float64(c.B) / 255,
		float64(c.A) / 255,
	}
}

func floatsToNRGBA(p [4]float64) color.NRGBA {
	q := func(v float64) uint8 {
		if v <= 0 {
			return 0
		}
		if v >= 1 {
			return 255
		}
		return uint8(math.Round(v * 255))
	}
	return color.NRGBA{q(p[0]), q(p[1]), q(p[2]), q(p[3])}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// TestBlendPixelAnchors checks blendPixel against exact hand-computed
// values. All values are binary-exact, so the results must match exactly.
func TestBlendPixelAnchors(t *testing.T) {
	pb := pixel{0.5, 0.25, 0.75, 1}
	ps := pixel{0.5, 0.5, 0.25, 1}
	testData := []struct {
		mode BlendMode
		want pixel
	}{
		{Normal, pixel{0.5, 0.5, 0.25, 1}},
		{Multiply, pixel{0.25, 0.125, 0.1875, 1}},
		{Screen, pixel{0.75, 0.625, 0.8125, 1}},
		{Overlay, pixel{0.5, 0.25, 0.625, 1}},
		{Darken, pixel{0.5, 0.25, 0.25, 1}},
		{Lighten, pixel{0.5, 0.5, 0.75, 1}},
		{ColorDodge, pixel{1, 0.5, 1, 1}},
		{ColorBurn, pixel{0, 0, 0, 1}},
		{HardLight, pixel{0.5, 0.25, 0.375, 1}},
		{SoftLight, pixel{0.5, 0.25, 0.65625, 1}},
		{Difference, pixel{0, 0.25, 0.5, 1}},
		{Exclusion, pixel{0.5, 0.5, 0.625, 1}},
		{Add, pixel{1, 0.75, 1, 1}},
		{Subtract, pixel{0, 0, 0.5, 1}},
	}
	for _, d := range testData {
		if got := blendPixel(d.mode, pb, ps); got != d.want {
			t.Errorf("blendPixel(%d, %v, %v): got %v, want %v", d.mode, pb, ps, got, d.want)
		}
	}

	// A fully transparent source leaves the backdrop untouched.
	for _, mode := range blendTestModes {
		tp := pixel{1, 1, 1, 0}
		if got := blendPixel(mode, pb, tp); got != pb {
			t.Errorf("blendPixel(%d, %v, %v): got %v, want %v", mode, pb, tp, got, pb)
		}
	}

	// A fully transparent backdrop lets the source pass through.
	for _, mode := range blendTestModes {
		tp := pixel{1, 1, 1, 0}
		sp := pixel{0.5, 0.5, 0.25, 0.8}
		if got := blendPixel(mode, tp, sp); got != sp {
			t.Errorf("blendPixel(%d, %v, %v): got %v, want %v", mode, tp, sp, got, sp)
		}
	}

	// Two fully transparent pixels produce a zeroed transparent pixel.
	for _, mode := range blendTestModes {
		p0 := pixel{1, 0.5, 0.25, 0}
		p1 := pixel{0.5, 1, 0.75, 0}
		if got := blendPixel(mode, p0, p1); got != (pixel{0, 0, 0, 0}) {
			t.Errorf("blendPixel(%d, %v, %v): got %v, want zero pixel", mode, p0, p1, got)
		}
	}
}

// TestBlendPixelReference checks blendPixel against the independent
// reference implementation on a fixed matrix of pixel pairs.
func TestBlendPixelReference(t *testing.T) {
	backdrops := []pixel{
		{0.2, 0.4, 0.6, 1},
		{0.9, 0.1, 0.5, 1},
		{0.3, 0.7, 0.2, 0.5},
		{0.8, 0.6, 0.4, 0.25},
		{0.5, 0.5, 0.5, 0},
	}
	sources := []pixel{
		{0.7, 0.3, 0.9, 1},
		{0.1, 0.8, 0.2, 0.6},
		{0.6, 0.4, 0.8, 0.3},
		{0.4, 0.4, 0.4, 0},
	}
	const tol = 1e-5
	for _, mode := range blendTestModes {
		for _, pb := range backdrops {
			for _, ps := range sources {
				got := blendPixel(mode, pb, ps)
				want := refBlendPixel(mode,
					[4]float64{float64(pb.r), float64(pb.g), float64(pb.b), float64(pb.a)},
					[4]float64{float64(ps.r), float64(ps.g), float64(ps.b), float64(ps.a)},
				)
				w := pixel{float32(want[0]), float32(want[1]), float32(want[2]), float32(want[3])}
				if absf32(got.r-w.r) > tol || absf32(got.g-w.g) > tol ||
					absf32(got.b-w.b) > tol || absf32(got.a-w.a) > tol {
					t.Errorf("blendPixel(%d, %v, %v): got %v, want %v", mode, pb, ps, got, w)
				}
			}
		}
	}
}

// TestBlendFilter runs the Blend filter over the fixed layer matrix and
// checks the result against the reference implementation.
func TestBlendFilter(t *testing.T) {
	backdrop := nrgbaImage(4, 3, blendTestBackdrop)
	layer := nrgbaImage(4, 3, blendTestSource)

	for _, mode := range blendTestModes {
		g := New(Blend(layer, mode))
		dst := image.NewNRGBA(g.Bounds(backdrop.Bounds()))
		g.Draw(dst, backdrop)

		// The serial and parallel results must be identical.
		g2 := New(Blend(layer, mode))
		g2.SetParallelization(false)
		dst2 := image.NewNRGBA(g2.Bounds(backdrop.Bounds()))
		g2.Draw(dst2, backdrop)
		for i := range dst.Pix {
			if dst.Pix[i] != dst2.Pix[i] {
				t.Fatalf("mode %d: parallel and serial results differ at byte %d", mode, i)
			}
		}

		for i := range blendTestBackdrop {
			want := floatsToNRGBA(refBlendPixel(mode,
				nrgbaToFloats(blendTestBackdrop[i]),
				nrgbaToFloats(blendTestSource[i]),
			))
			got := dst.Pix[i*4 : i*4+4]
			w := []uint8{want.R, want.G, want.B, want.A}
			for c := 0; c < 4; c++ {
				d := int(got[c]) - int(w[c])
				if d < -1 || d > 1 {
					t.Errorf("mode %d, pixel %d, channel %d: got %d, want %d", mode, i, c, got[c], w[c])
				}
			}
		}
	}
}

// TestBlendNormalVsDrawOver checks that the Normal blend mode agrees with
// the standard library's draw.Over compositing on the fixed layer matrix.
func TestBlendNormalVsDrawOver(t *testing.T) {
	backdrop := nrgbaImage(4, 3, blendTestBackdrop)
	layer := nrgbaImage(4, 3, blendTestSource)

	g := New(Blend(layer, Normal))
	dst := image.NewNRGBA(g.Bounds(backdrop.Bounds()))
	g.Draw(dst, backdrop)

	want := nrgbaImage(4, 3, blendTestBackdrop)
	draw.Draw(want, want.Bounds(), layer, image.Point{}, draw.Over)

	for i := 0; i < 12; i++ {
		got := dst.Pix[i*4 : i*4+4]
		w := want.Pix[i*4 : i*4+4]
		if w[3] == 0 {
			// Fully transparent pixel: only the alpha channel is significant.
			if got[3] != 0 {
				t.Errorf("pixel %d: got alpha %d, want 0", i, got[3])
			}
			continue
		}
		for c := 0; c < 4; c++ {
			d := int(got[c]) - int(w[c])
			if d < -1 || d > 1 {
				t.Errorf("pixel %d, channel %d: got %d, want %d", i, c, got[c], w[c])
			}
		}
	}
}

// TestBlendDrawAtOverConsistency checks that the Normal blend mode agrees
// with the DrawAt OverOperator composition on the fixed layer matrix.
func TestBlendDrawAtOverConsistency(t *testing.T) {
	backdrop := nrgbaImage(4, 3, blendTestBackdrop)
	layer := nrgbaImage(4, 3, blendTestSource)

	g := New(Blend(layer, Normal))
	dst := image.NewNRGBA(g.Bounds(backdrop.Bounds()))
	g.Draw(dst, backdrop)

	want := nrgbaImage(4, 3, blendTestBackdrop)
	New().DrawAt(want, layer, image.Point{}, OverOperator)

	for i := 0; i < 12; i++ {
		if blendTestBackdrop[i].A == 0 && blendTestSource[i].A == 0 {
			continue
		}
		got := dst.Pix[i*4 : i*4+4]
		w := want.Pix[i*4 : i*4+4]
		for c := 0; c < 4; c++ {
			d := int(got[c]) - int(w[c])
			if d < -1 || d > 1 {
				t.Errorf("pixel %d, channel %d: got %d, want %d", i, c, got[c], w[c])
			}
		}
	}
}

// TestBlendOpaquePath checks that blending two opaque images produces an
// opaque image and that the Normal mode output equals the top layer.
func TestBlendOpaquePath(t *testing.T) {
	backdrop := nrgbaImage(4, 3, blendTestBackdrop)
	layer := nrgbaImage(4, 3, blendTestSource)
	// Make the fixed matrix fully opaque.
	for i := 3; i < len(backdrop.Pix); i += 4 {
		backdrop.Pix[i] = 255
	}
	for i := 3; i < len(layer.Pix); i += 4 {
		layer.Pix[i] = 255
	}

	for _, mode := range blendTestModes {
		g := New(Blend(layer, mode))
		dst := image.NewNRGBA(g.Bounds(backdrop.Bounds()))
		g.Draw(dst, backdrop)
		for i := 3; i < len(dst.Pix); i += 4 {
			if dst.Pix[i] != 255 {
				t.Errorf("mode %d, pixel %d: got alpha %d, want 255", mode, i/4, dst.Pix[i])
			}
		}
	}

	// Normal mode with an opaque layer is a plain copy of the layer.
	g := New(Blend(layer, Normal))
	dst := image.NewNRGBA(g.Bounds(backdrop.Bounds()))
	g.Draw(dst, backdrop)
	for i := range dst.Pix {
		if dst.Pix[i] != layer.Pix[i] {
			t.Fatalf("normal opaque blend differs from the layer at byte %d: got %d, want %d",
				i, dst.Pix[i], layer.Pix[i])
		}
	}

	// The result equals the empty filter list (copy) of the layer.
	want := image.NewNRGBA(g.Bounds(layer.Bounds()))
	New().Draw(want, layer)
	for i := range dst.Pix {
		if dst.Pix[i] != want.Pix[i] {
			t.Fatalf("normal opaque blend differs from copy at byte %d", i)
		}
	}
}

// TestBlendNoBleed checks that the colors of transparent pixels never
// affect visible pixels.
func TestBlendNoBleed(t *testing.T) {
	// Opaque backdrop, fully transparent layer with "dirty" hidden colors.
	backdrop := nrgbaImage(2, 2, []color.NRGBA{
		{10, 20, 30, 255}, {40, 50, 60, 255},
		{70, 80, 90, 255}, {100, 110, 120, 255},
	})
	layer := nrgbaImage(2, 2, []color.NRGBA{
		{255, 0, 0, 0}, {0, 255, 0, 0},
		{0, 0, 255, 0}, {255, 255, 255, 0},
	})
	for _, mode := range blendTestModes {
		g := New(Blend(layer, mode))
		dst := image.NewNRGBA(g.Bounds(backdrop.Bounds()))
		g.Draw(dst, backdrop)
		for i := range dst.Pix {
			if dst.Pix[i] != backdrop.Pix[i] {
				t.Errorf("mode %d: transparent layer changed the backdrop at byte %d: got %d, want %d",
					mode, i, dst.Pix[i], backdrop.Pix[i])
			}
		}
	}

	// Two fully transparent layers produce zeroed transparent pixels.
	backdrop2 := nrgbaImage(2, 2, []color.NRGBA{
		{255, 0, 0, 0}, {0, 255, 0, 0},
		{0, 0, 255, 0}, {255, 255, 255, 0},
	})
	for _, mode := range blendTestModes {
		g := New(Blend(layer, mode))
		dst := image.NewNRGBA(g.Bounds(backdrop2.Bounds()))
		g.Draw(dst, backdrop2)
		for i := range dst.Pix {
			if dst.Pix[i] != 0 {
				t.Errorf("mode %d: expected zeroed transparent pixel, got %d at byte %d", mode, dst.Pix[i], i)
			}
		}
	}
}

// TestBlendPipeline checks the composition of the Blend filter with the
// pixel processing pipeline (colors/pixels) against the reference.
func TestBlendPipeline(t *testing.T) {
	backdrop := nrgbaImage(4, 3, blendTestBackdrop)
	layer := nrgbaImage(4, 3, blendTestSource)

	// A colors filter (Brightness) followed by blending.
	g := New(Brightness(20), Blend(layer, Multiply))
	dst := image.NewNRGBA(g.Bounds(backdrop.Bounds()))
	g.Draw(dst, backdrop)
	for i := range blendTestBackdrop {
		b := nrgbaToFloats(blendTestBackdrop[i])
		for c := 0; c < 3; c++ {
			b[c] = clamp01(b[c] + 0.2)
		}
		want := floatsToNRGBA(refBlendPixel(Multiply, b, nrgbaToFloats(blendTestSource[i])))
		got := dst.Pix[i*4 : i*4+4]
		w := []uint8{want.R, want.G, want.B, want.A}
		for c := 0; c < 4; c++ {
			d := int(got[c]) - int(w[c])
			if d < -1 || d > 1 {
				t.Errorf("brightness+multiply: pixel %d, channel %d: got %d, want %d", i, c, got[c], w[c])
			}
		}
	}

	// Blending followed by a colors filter (Invert).
	g = New(Blend(layer, Screen), Invert())
	dst = image.NewNRGBA(g.Bounds(backdrop.Bounds()))
	g.Draw(dst, backdrop)
	for i := range blendTestBackdrop {
		r := refBlendPixel(Screen, nrgbaToFloats(blendTestBackdrop[i]), nrgbaToFloats(blendTestSource[i]))
		for c := 0; c < 3; c++ {
			r[c] = 1 - r[c]
		}
		want := floatsToNRGBA(r)
		got := dst.Pix[i*4 : i*4+4]
		w := []uint8{want.R, want.G, want.B, want.A}
		for c := 0; c < 4; c++ {
			d := int(got[c]) - int(w[c])
			if d < -1 || d > 1 {
				t.Errorf("screen+invert: pixel %d, channel %d: got %d, want %d", i, c, got[c], w[c])
			}
		}
	}
}

// TestBlendPremultiplied checks that premultiplied images (*image.RGBA)
// are handled consistently with straight images (*image.NRGBA).
func TestBlendPremultiplied(t *testing.T) {
	// Use only high-alpha pixels to keep the 8-bit premultiplied
	// round-trip error small.
	straight := []color.NRGBA{
		{10, 20, 30, 255}, {200, 100, 50, 255},
		{255, 0, 0, 128}, {0, 255, 0, 200},
	}
	backdropN := nrgbaImage(2, 2, straight)
	layerN := nrgbaImage(2, 2, []color.NRGBA{
		{250, 240, 230, 255}, {64, 192, 128, 255},
		{0, 255, 255, 128}, {33, 66, 99, 160},
	})

	backdropP := image.NewRGBA(backdropN.Bounds())
	draw.Draw(backdropP, backdropP.Bounds(), backdropN, image.Point{}, draw.Src)
	layerP := image.NewRGBA(layerN.Bounds())
	draw.Draw(layerP, layerP.Bounds(), layerN, image.Point{}, draw.Src)

	for _, mode := range blendTestModes {
		g1 := New(Blend(layerN, mode))
		want := image.NewNRGBA(g1.Bounds(backdropN.Bounds()))
		g1.Draw(want, backdropN)

		g2 := New(Blend(layerP, mode))
		got := image.NewNRGBA(g2.Bounds(backdropP.Bounds()))
		g2.Draw(got, backdropP)

		for i := range want.Pix {
			d := int(got.Pix[i]) - int(want.Pix[i])
			if d < -2 || d > 2 {
				t.Errorf("mode %d, byte %d: premultiplied %d, straight %d", mode, i, got.Pix[i], want.Pix[i])
			}
		}
	}

	// Premultiplied destination image.
	g := New(Blend(layerN, Multiply))
	dstN := image.NewNRGBA(g.Bounds(backdropN.Bounds()))
	g.Draw(dstN, backdropN)
	dstP := image.NewRGBA(g.Bounds(backdropN.Bounds()))
	g.Draw(dstP, backdropN)
	pg := newPixelGetter(dstP)
	for i := 0; i < 4; i++ {
		x := i % 2
		y := i / 2
		px := pg.getPixel(x, y)
		want := refBlendPixel(Multiply, nrgbaToFloats(straight[i]), nrgbaToFloats(layerN.NRGBAAt(x, y)))
		const tol = 2.0 / 255
		if absf32(px.r-float32(want[0])) > tol || absf32(px.g-float32(want[1])) > tol ||
			absf32(px.b-float32(want[2])) > tol || absf32(px.a-float32(want[3])) > tol {
			t.Errorf("rgba dst: pixel %d: got %v, want %v", i, px, want)
		}
	}
	_ = dstN
}

// TestBlendBounds checks the Bounds method of the Blend filter.
func TestBlendBounds(t *testing.T) {
	layer := nrgbaImage(2, 2, make([]color.NRGBA, 4))
	g := New(Blend(layer, Normal))
	b := g.Bounds(image.Rect(5, 6, 9, 9))
	if !b.Eq(image.Rect(0, 0, 4, 3)) {
		t.Errorf("unexpected bounds: %v", b)
	}
}

// TestBlendAlignment checks that the top-left corner of the layer is
// aligned with the top-left corner of the destination image and that
// pixels not covered by the layer are left unchanged.
func TestBlendAlignment(t *testing.T) {
	// 2x2 layer with a non-zero Min point.
	layer := image.NewNRGBA(image.Rect(2, 3, 4, 5))
	for i := range layer.Pix {
		layer.Pix[i] = 255
	}
	opaqueBlack := make([]color.NRGBA, 16)
	for i := range opaqueBlack {
		opaqueBlack[i] = color.NRGBA{0, 0, 0, 255}
	}
	backdrop := nrgbaImage(4, 4, opaqueBlack)

	g := New(Blend(layer, Normal))
	dst := image.NewNRGBA(g.Bounds(backdrop.Bounds()))
	g.Draw(dst, backdrop)

	red := []uint8{255, 255, 255, 255}
	black := []uint8{0, 0, 0, 255}
	want := [][]uint8{
		red, red, black, black,
		red, red, black, black,
		black, black, black, black,
		black, black, black, black,
	}
	for i := 0; i < 16; i++ {
		got := dst.Pix[i*4 : i*4+4]
		for c := 0; c < 4; c++ {
			if got[c] != want[i][c] {
				t.Errorf("pixel %d, channel %d: got %d, want %d", i, c, got[c], want[i][c])
			}
		}
	}

	// Drawing into a larger destination image leaves the uncovered
	// pixels untouched.
	dst2 := image.NewNRGBA(image.Rect(0, 0, 6, 6))
	for i := range dst2.Pix {
		dst2.Pix[i] = 9
	}
	g.Draw(dst2, backdrop)
	for y := 0; y < 6; y++ {
		for x := 0; x < 6; x++ {
			if x < 4 && y < 4 {
				continue
			}
			i := dst2.PixOffset(x, y)
			for c := 0; c < 4; c++ {
				if dst2.Pix[i+c] != 9 {
					t.Errorf("pixel (%d, %d) channel %d: got %d, want 9", x, y, c, dst2.Pix[i+c])
				}
			}
		}
	}
}
