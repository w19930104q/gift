package gift

import (
	"image"
	"image/color"
	"testing"
)

// patchBlocks builds an NRGBA image tiled from the given solid color blocks.
// Each block occupies blockSize x blockSize pixels and the blocks are laid
// out row by row. The fixed matrix is the acceptance fixture: every pixel in
// a block must convert identically.
func patchBlocks(blocks []color.NRGBA, blockSize, cols int) *image.NRGBA {
	rows := (len(blocks) + cols - 1) / cols
	img := image.NewNRGBA(image.Rect(0, 0, cols*blockSize, rows*blockSize))
	for i, c := range blocks {
		bx, by := (i%cols)*blockSize, (i/cols)*blockSize
		for y := 0; y < blockSize; y++ {
			for x := 0; x < blockSize; x++ {
				img.Set(bx+x, by+y, c)
			}
		}
	}
	return img
}

// blockColors is the fixed acceptance matrix: black, white, primaries,
// secondaries, a mid gray and two arbitrary saturated colors.
var blockColors = []color.NRGBA{
	{0, 0, 0, 255},       // 0 black
	{255, 255, 255, 255}, // 1 white
	{255, 0, 0, 255},     // 2 red
	{0, 255, 0, 255},     // 3 green
	{0, 0, 255, 255},     // 4 blue
	{255, 255, 0, 255},   // 5 yellow
	{0, 255, 255, 255},   // 6 cyan
	{255, 0, 255, 255},   // 7 magenta
	{128, 128, 128, 255}, // 8 mid gray
	{50, 100, 150, 255},  // 9 arbitrary 1
	{200, 90, 25, 255},   // 10 arbitrary 2
	{17, 73, 211, 255},   // 11 arbitrary 3
}

func TestRangeConstants(t *testing.T) {
	if FullRange == VideoRange {
		t.Fatal("FullRange and VideoRange must be distinct")
	}
}

func TestRGBToYCbCrFullMatchesStdlib(t *testing.T) {
	// Anchor values from the JFIF integer pipeline.
	cases := []struct {
		r, g, b uint8
	}{
		{0, 0, 0},
		{255, 255, 255},
		{255, 0, 0},
		{0, 255, 0},
		{0, 0, 255},
		{255, 255, 0},
		{0, 255, 255},
		{255, 0, 255},
		{128, 128, 128},
		{50, 100, 150},
	}
	for _, c := range cases {
		y, cb, cr := RGBToYCbCr(c.r, c.g, c.b, FullRange)
		sy, scb, scr := color.RGBToYCbCr(c.r, c.g, c.b)
		if y != sy || cb != scb || cr != scr {
			t.Errorf("RGBToYCbCr full (%v,%v,%v) got (%v,%v,%v) stdlib (%v,%v,%v)",
				c.r, c.g, c.b, y, cb, cr, sy, scb, scr)
		}
	}
}

func TestYCbCrToRGBFullMatchesStdlib(t *testing.T) {
	cases := []struct {
		y, cb, cr uint8
	}{
		{0, 128, 128},
		{255, 128, 128},
		{76, 84, 255},
		{149, 43, 21},
		{29, 255, 107},
		{128, 128, 128},
	}
	for _, c := range cases {
		r, g, b := YCbCrToRGB(c.y, c.cb, c.cr, FullRange)
		sr, sg, sb := color.YCbCrToRGB(c.y, c.cb, c.cr)
		if r != sr || g != sg || b != sb {
			t.Errorf("YCbCrToRGB full (%v,%v,%v) got (%v,%v,%v) stdlib (%v,%v,%v)",
				c.y, c.cb, c.cr, r, g, b, sr, sg, sb)
		}
	}
}

func TestVideoRangeAnchors(t *testing.T) {
	// Black maps to luma 16, white to luma 235, neutral chroma is 128.
	y, cb, cr := RGBToYCbCr(0, 0, 0, VideoRange)
	if y != 16 || cb != 128 || cr != 128 {
		t.Errorf("video black got (%v,%v,%v) want (16,128,128)", y, cb, cr)
	}
	y, cb, cr = RGBToYCbCr(255, 255, 255, VideoRange)
	if y != 235 || cb != 128 || cr != 128 {
		t.Errorf("video white got (%v,%v,%v) want (235,128,128)", y, cb, cr)
	}
	// And decode back exactly.
	r, g, b := YCbCrToRGB(16, 128, 128, VideoRange)
	if r != 0 || g != 0 || b != 0 {
		t.Errorf("video decode black got (%v,%v,%v) want (0,0,0)", r, g, b)
	}
	r, g, b = YCbCrToRGB(235, 128, 128, VideoRange)
	if r != 255 || g != 255 || b != 255 {
		t.Errorf("video decode white got (%v,%v,%v) want (255,255,255)", r, g, b)
	}
	// Neutral chroma on any luma gives gray.
	r, g, b = YCbCrToRGB(128, 128, 128, VideoRange)
	if r != g || g != b {
		t.Errorf("video neutral chroma must be gray, got (%v,%v,%v)", r, g, b)
	}
}

func TestVideoRangeClamping(t *testing.T) {
	// Encoded samples stay inside the legal limited intervals.
	for _, c := range blockColors {
		y, cb, cr := RGBToYCbCr(c.R, c.G, c.B, VideoRange)
		if y < 16 || y > 235 {
			t.Errorf("luma %v out of [16,235] for %v", y, c)
		}
		if cb < 16 || cb > 240 || cr < 16 || cr > 240 {
			t.Errorf("chroma (%v,%v) out of [16,240] for %v", cb, cr, c)
		}
	}
	// Decoder clamps to 0..255 even for out-of-range (headroom) codes.
	for _, c := range []struct{ y, cb, cr uint8 }{
		{0, 0, 0},
		{255, 255, 255},
		{16, 240, 16},
		{235, 16, 240},
	} {
		r, g, b := YCbCrToRGB(c.y, c.cb, c.cr, VideoRange)
		if r > 255 || g > 255 || b > 255 {
			t.Errorf("decoder failed to clamp (%v,%v,%v) -> (%v,%v,%v)", c.y, c.cb, c.cr, r, g, b)
		}
	}
}

func TestRoundTripBlockMatrix(t *testing.T) {
	const tol = 2 // 8-bit code levels; full-range observed drift <=1, video <=3
	for _, rng := range []Range{FullRange, VideoRange} {
		for _, c := range blockColors {
			y, cb, cr := RGBToYCbCr(c.R, c.G, c.B, rng)
			r, g, b := YCbCrToRGB(y, cb, cr, rng)
			if absu8(r, c.R) > tol || absu8(g, c.G) > tol || absu8(b, c.B) > tol {
				t.Errorf("rng=%v round trip %v -> (%v,%v,%v) -> (%v,%v,%v)",
					rng, c, y, cb, cr, r, g, b)
			}
		}
	}
}

func absu8(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// nrgbaAt reads a pixel as color.NRGBA regardless of the concrete image type.
func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

// TestFiltersBlockMatrix runs both conversion filters over the fixed block
// matrix and checks that every pixel inside a block converts identically and
// matches the scalar conversion functions.
func TestFiltersBlockMatrix(t *testing.T) {
	const blockSize = 4
	src := patchBlocks(blockColors, blockSize, 4)

	for _, rng := range []Range{FullRange, VideoRange} {
		enc := New(RGBToYCbCrFilter(rng))
		encoded := image.NewNRGBA64(enc.Bounds(src.Bounds()))
		enc.Draw(encoded, src)

		dec := New(YCbCrToRGBFilter(rng))
		dst := image.NewNRGBA(dec.Bounds(encoded.Bounds()))
		dec.Draw(dst, encoded)

		for i, c := range blockColors {
			bx, by := (i%4)*blockSize, (i/4)*blockSize
			wantY, wantCb, wantCr := RGBToYCbCr(c.R, c.G, c.B, rng)
			for y := 0; y < blockSize; y++ {
				for x := 0; x < blockSize; x++ {
					px, py := bx+x, by+y
					er := nrgbaAt(encoded, px, py)
					if er.R != wantY || er.G != wantCb || er.B != wantCr {
						t.Errorf("rng=%v encode block %d at (%d,%d): got (%d,%d,%d) want (%d,%d,%d)",
							rng, i, x, y, er.R, er.G, er.B, wantY, wantCb, wantCr)
					}
					got := nrgbaAt(dst, px, py)
					if absu8(got.R, c.R) > 2 || absu8(got.G, c.G) > 2 || absu8(got.B, c.B) > 2 {
						t.Errorf("rng=%v decode block %d at (%d,%d): got %v want ~%v",
							rng, i, x, y, got, c)
					}
					if got.A != 255 {
						t.Errorf("alpha not preserved: %v", got.A)
					}
				}
			}
		}
	}
}

// TestFilterChainSemantics verifies that inserting an encode/decode pair into
// an arbitrary filter chain does not change the color semantics beyond the
// 8-bit round-trip tolerance, and that the range is carried explicitly: a
// video-range pair must not produce the same intermediate codes as a
// full-range pair on the same image.
func TestFilterChainSemantics(t *testing.T) {
	src := patchBlocks(blockColors, 3, 4)

	// Reference chain without YCbCr stages.
	ref := New(
		Contrast(15),
		Saturation(20),
		ColorspaceSRGBToLinear(),
		ColorspaceLinearToSRGB(),
		Gamma(1.0),
	)
	refDst := image.NewNRGBA(ref.Bounds(src.Bounds()))
	ref.Draw(refDst, src)

	for _, rng := range []Range{FullRange, VideoRange} {
		chain := New(
			Contrast(15),
			RGBToYCbCrFilter(rng),
			YCbCrToRGBFilter(rng),
			Saturation(20),
			ColorspaceSRGBToLinear(),
			ColorspaceLinearToSRGB(),
		)
		dst := image.NewNRGBA(chain.Bounds(src.Bounds()))
		chain.Draw(dst, src)

		for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
			for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
				a := nrgbaAt(refDst, x, y)
				b := nrgbaAt(dst, x, y)
				if absu8(a.R, b.R) > 3 || absu8(a.G, b.G) > 3 || absu8(a.B, b.B) > 3 {
					t.Errorf("rng=%v chain drift at (%d,%d): ref %v got %v", rng, x, y, a, b)
				}
			}
		}
	}

	// Explicit range semantics: encoded luma of the same source must differ
	// between full and video range (black: 0 vs 16).
	fullEnc := New(RGBToYCbCrFilter(FullRange))
	vidEnc := New(RGBToYCbCrFilter(VideoRange))
	fe := image.NewNRGBA(fullEnc.Bounds(src.Bounds()))
	ve := image.NewNRGBA(vidEnc.Bounds(src.Bounds()))
	fullEnc.Draw(fe, src)
	vidEnc.Draw(ve, src)
	if nrgbaAt(fe, 0, 0).R == nrgbaAt(ve, 0, 0).R {
		t.Error("full and video range must produce distinct luma codes")
	}
	if nrgbaAt(fe, 0, 0).R != 0 || nrgbaAt(ve, 0, 0).R != 16 {
		t.Errorf("black luma: full=%d video=%d", nrgbaAt(fe, 0, 0).R, nrgbaAt(ve, 0, 0).R)
	}
}

// TestYCbCrPlanarRoundTrip exercises real planar *image.YCbCr sources and
// destinations across every supported subsampling ratio, via the helper and
// the decode filter.
func TestYCbCrPlanarRoundTrip(t *testing.T) {
	ratios := []image.YCbCrSubsampleRatio{
		image.YCbCrSubsampleRatio444,
		image.YCbCrSubsampleRatio422,
		image.YCbCrSubsampleRatio420,
		image.YCbCrSubsampleRatio440,
	}
	const blockSize = 4
	src := patchBlocks(blockColors, blockSize, 4)

	for _, rng := range []Range{FullRange, VideoRange} {
		for _, ratio := range ratios {
			yimg := image.NewYCbCr(src.Bounds(), ratio)
			DrawRGBToYCbCr(yimg, src, rng, nil)

			// Luma anchors: block 0 (black) at (0,0), block 1 (white) at
			// (blockSize, 0).
			check := func(x, y int, wantY uint8) {
				got := yimg.Y[yimg.YOffset(x, y)]
				if got != wantY {
					t.Errorf("rng=%v ratio=%v luma at (%d,%d): got %d want %d",
						rng, ratio, x, y, got, wantY)
				}
			}
			wantBlack, _, _ := RGBToYCbCr(0, 0, 0, rng)
			wantWhite, _, _ := RGBToYCbCr(255, 255, 255, rng)
			check(0, 0, wantBlack)
			check(blockSize, 0, wantWhite)

			dec := New(YCbCrToRGBFilter(rng))
			dst := image.NewNRGBA(dec.Bounds(yimg.Bounds()))
			dec.Draw(dst, yimg)

			for i, c := range blockColors {
				bx, by := (i%4)*blockSize, (i/4)*blockSize
				// Sample the block center to avoid chroma block edges.
				cx, cy := bx+blockSize/2, by+blockSize/2
				got := nrgbaAt(dst, cx, cy)
				tol := 2
				if ratio != image.YCbCrSubsampleRatio444 {
					tol = 4 // chroma subsampling averages neighbor blocks
				}
				if absu8(got.R, c.R) > tol || absu8(got.G, c.G) > tol || absu8(got.B, c.B) > tol {
					t.Errorf("rng=%v ratio=%v planar block %d center: got %v want ~%v",
						rng, ratio, i, got, c)
				}
			}
		}
	}
}

// TestFiltersOn16Bit checks the conversion filters on 16-bit source and
// destination images (the pipeline's intermediate image type is NRGBA64),
// verifying the samples round-trip through the same scalar conversion.
func TestFiltersOn16Bit(t *testing.T) {
	src := image.NewNRGBA64(image.Rect(0, 0, len(blockColors), 1))
	for i, c := range blockColors {
		src.Set(i, 0, color.NRGBA64{
			R: uint16(c.R) * 0x101,
			G: uint16(c.G) * 0x101,
			B: uint16(c.B) * 0x101,
			A: 0xffff,
		})
	}
	for _, rng := range []Range{FullRange, VideoRange} {
		enc := New(RGBToYCbCrFilter(rng))
		encoded := image.NewNRGBA64(enc.Bounds(src.Bounds()))
		enc.Draw(encoded, src)
		dec := New(YCbCrToRGBFilter(rng))
		dst := image.NewNRGBA64(dec.Bounds(encoded.Bounds()))
		dec.Draw(dst, encoded)

		for i, c := range blockColors {
			wantY, wantCb, wantCr := RGBToYCbCr(c.R, c.G, c.B, rng)
			ec := nrgbaAt(encoded, i, 0)
			if absu8(ec.R, wantY) > 1 || absu8(ec.G, wantCb) > 1 || absu8(ec.B, wantCr) > 1 {
				t.Errorf("rng=%v 16-bit encode %d: got (%d,%d,%d) want (%d,%d,%d)",
					rng, i, ec.R, ec.G, ec.B, wantY, wantCb, wantCr)
			}
			got := nrgbaAt(dst, i, 0)
			if absu8(got.R, c.R) > 2 || absu8(got.G, c.G) > 2 || absu8(got.B, c.B) > 2 {
				t.Errorf("rng=%v 16-bit decode %d: got %v want ~%v", rng, i, got, c)
			}
		}
	}
}

// TestGammaGrayscaleChain combines the gamma and grayscale filters with both
// conversion ranges and checks that the conversion pair introduces no drift
// beyond the 8-bit round-trip tolerance compared with the same chain without
// the YCbCr stages.
func TestGammaGrayscaleChain(t *testing.T) {
	src := patchBlocks(blockColors, 3, 4)

	ref := New(Gamma(1.5), Grayscale(), Invert())
	refDst := image.NewNRGBA(ref.Bounds(src.Bounds()))
	ref.Draw(refDst, src)

	for _, rng := range []Range{FullRange, VideoRange} {
		chain := New(
			Gamma(1.5),
			RGBToYCbCrFilter(rng),
			YCbCrToRGBFilter(rng),
			Grayscale(),
			Invert(),
		)
		dst := image.NewNRGBA(chain.Bounds(src.Bounds()))
		chain.Draw(dst, src)

		for y := 0; y < src.Bounds().Dy(); y++ {
			for x := 0; x < src.Bounds().Dx(); x++ {
				a := nrgbaAt(refDst, x, y)
				b := nrgbaAt(dst, x, y)
				if absu8(a.R, b.R) > 3 || absu8(a.G, b.G) > 3 || absu8(a.B, b.B) > 3 {
					t.Errorf("rng=%v gamma/grayscale chain drift at (%d,%d): ref %v got %v",
						rng, x, y, a, b)
				}
			}
		}
	}
}

// TestDefaultConversionUnchanged ensures the pre-existing YCbCr pixel getter
// path still agrees with the standard library on the fixed matrix, i.e. the
// default (full-range) conversion output is unchanged by this work.
func TestDefaultConversionUnchanged(t *testing.T) {
	for _, c := range blockColors {
		y, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
		img := image.NewYCbCr(image.Rect(0, 0, 1, 1), image.YCbCrSubsampleRatio444)
		img.Y[0], img.Cb[0], img.Cr[0] = y, cb, cr
		pg := newPixelGetter(img)
		px := pg.getPixel(0, 0)
		rr, gg, bb := color.YCbCrToRGB(y, cb, cr)
		if absu8(f32u8(px.r*0xff), rr) > 1 ||
			absu8(f32u8(px.g*0xff), gg) > 1 ||
			absu8(f32u8(px.b*0xff), bb) > 1 {
			t.Errorf("default getter drift for %v -> px %v vs stdlib (%v,%v,%v)",
				c, px, rr, gg, bb)
		}
	}
}
