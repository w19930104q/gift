package gift

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"sync"
	"testing"
)

// chainTestMatrix is the fixed acceptance matrix for the filter chain
// semantics documented in the package comment:
//
//  1. Chain order equals execution order, and the result is reproducible.
//  2. Region access inside a chain is bounded: filters never read outside
//     their input bounds, never write outside their output bounds, never
//     share pixel buffers between stages and never modify src.
//  3. Parallel and serial execution produce pixel-identical results.
//  4. A single filter inside a chain behaves exactly as the bare filter.
//
// Each entry constructs a fresh filter list so that matrix entries cannot
// leak state into one another.
var chainTestMatrix = []struct {
	name     string
	newChain func() []Filter
}{
	{"empty", func() []Filter { return nil }},
	{"invert", func() []Filter { return []Filter{Invert()} }},
	{"resize_linear", func() []Filter {
		return []Filter{Resize(24, 18, LinearResampling)}
	}},
	{"gaussian_blur", func() []Filter { return []Filter{GaussianBlur(1)} }},
	{"rotate90", func() []Filter { return []Filter{Rotate90()} }},
	{"median", func() []Filter { return []Filter{Median(3, true)} }},
	{"invert>brightness", func() []Filter {
		return []Filter{Invert(), Brightness(20)}
	}},
	{"brightness>invert", func() []Filter {
		return []Filter{Brightness(20), Invert()}
	}},
	{"rotate90>flip_horizontal", func() []Filter {
		return []Filter{Rotate90(), FlipHorizontal()}
	}},
	{"flip_horizontal>rotate90", func() []Filter {
		return []Filter{FlipHorizontal(), Rotate90()}
	}},
	{"resize>unsharp>grayscale", func() []Filter {
		return []Filter{Resize(32, 0, LanczosResampling), UnsharpMask(1, 1, 0), Grayscale()}
	}},
	{"crop>blur>sobel", func() []Filter {
		return []Filter{Crop(image.Rect(2, 3, 40, 30)), GaussianBlur(1.5), Sobel()}
	}},
	{"convolution>contrast>rotate180", func() []Filter {
		return []Filter{
			Convolution([]float32{-1, -1, 0, -1, 1, 1, 0, 1, 1}, false, false, false, 0),
			Contrast(10),
			Rotate180(),
		}
	}},
	{"median>pixelate>hue", func() []Filter {
		return []Filter{Median(3, true), Pixelate(3), Hue(45)}
	}},
	{"resize_fill>gamma>colorspace_roundtrip", func() []Filter {
		return []Filter{
			ResizeToFill(30, 20, NearestNeighborResampling, CenterAnchor),
			Gamma(1.3),
			ColorspaceSRGBToLinear(),
			ColorspaceLinearToSRGB(),
		}
	}},
}

// chainTestSrc generates a deterministic pseudo-random source image with the
// given bounds. The fixed seed makes every test run reproducible.
func chainTestSrc(b image.Rectangle) *image.NRGBA {
	src := image.NewNRGBA(b)
	seed := uint32(0x9e3779b9)
	for i := range src.Pix {
		seed = seed*1664525 + 1013904223
		src.Pix[i] = uint8(seed >> 24)
	}
	return src
}

// chainDraw applies the filter chain to src with the given parallelization
// setting and returns the resulting NRGBA image.
func chainDraw(filters []Filter, src image.Image, parallel bool) *image.NRGBA {
	g := New(filters...)
	g.SetParallelization(parallel)
	dst := image.NewNRGBA(g.Bounds(src.Bounds()))
	g.Draw(dst, src)
	return dst
}

// referenceChainDraw applies the filters to src one by one, independently of
// the GIFT.Draw implementation: each filter draws into a plain temporary
// image, the last one draws directly into dst. It encodes the documented
// chain semantics: list order is execution order, stages are composed through
// intermediate images, and nothing is reordered, merged or skipped.
func referenceChainDraw(dst draw.Image, src image.Image, filters []Filter, options *Options) {
	if len(filters) == 0 {
		draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
		return
	}
	in := src
	for i, f := range filters {
		var out draw.Image
		if i == len(filters)-1 {
			out = dst
		} else {
			out = image.NewNRGBA64(f.Bounds(in.Bounds()))
		}
		f.Draw(out, in, options)
		in = out
	}
}

// TestChainOrderIsExecutionOrder verifies that a GIFT chain produces exactly
// the same pixels as the filters applied one by one in list order.
func TestChainOrderIsExecutionOrder(t *testing.T) {
	src := chainTestSrc(image.Rect(0, 0, 48, 36))
	for _, parallel := range []bool{true, false} {
		for _, d := range chainTestMatrix {
			name := fmt.Sprintf("%s/parallel=%v", d.name, parallel)
			filters := d.newChain()

			got := chainDraw(filters, src, parallel)

			g := New(d.newChain()...)
			want := image.NewNRGBA(g.Bounds(src.Bounds()))
			referenceChainDraw(want, src, d.newChain(), &Options{Parallelization: parallel})

			if !got.Bounds().Eq(want.Bounds()) {
				t.Errorf("%s: bounds mismatch: got %v, want %v", name, got.Bounds(), want.Bounds())
				continue
			}
			if !bytes.Equal(got.Pix, want.Pix) {
				t.Errorf("%s: chain result differs from sequential in-order filter composition", name)
			}
		}
	}
}

// TestChainOrderMatters verifies that the chain order is observable: swapping
// non-commuting filters changes the result. Together with
// TestChainOrderIsExecutionOrder this locks the list order as the execution
// order.
func TestChainOrderMatters(t *testing.T) {
	src := chainTestSrc(image.Rect(0, 0, 48, 36))
	pairs := [][2]string{
		{"invert>brightness", "brightness>invert"},
		{"rotate90>flip_horizontal", "flip_horizontal>rotate90"},
	}
	results := map[string]*image.NRGBA{}
	for _, d := range chainTestMatrix {
		results[d.name] = chainDraw(d.newChain(), src, true)
	}
	for _, pair := range pairs {
		a, b := results[pair[0]], results[pair[1]]
		if a.Bounds().Eq(b.Bounds()) && bytes.Equal(a.Pix, b.Pix) {
			t.Errorf("filter order is not observable: %q and %q produce identical results", pair[0], pair[1])
		}
	}
}

// TestChainParallelMatchesSerial verifies that parallel and serial execution
// of the same chain produce pixel-identical results.
func TestChainParallelMatchesSerial(t *testing.T) {
	src := chainTestSrc(image.Rect(0, 0, 48, 36))
	for _, d := range chainTestMatrix {
		par := chainDraw(d.newChain(), src, true)
		ser := chainDraw(d.newChain(), src, false)
		if !par.Bounds().Eq(ser.Bounds()) {
			t.Errorf("%s: bounds mismatch: parallel %v, serial %v", d.name, par.Bounds(), ser.Bounds())
			continue
		}
		if !bytes.Equal(par.Pix, ser.Pix) {
			t.Errorf("%s: parallel and serial results differ", d.name)
		}
	}
}

// TestChainReproducible verifies that applying the same chain to the same
// source image twice produces exactly the same pixels.
func TestChainReproducible(t *testing.T) {
	src := chainTestSrc(image.Rect(0, 0, 48, 36))
	for _, d := range chainTestMatrix {
		first := chainDraw(d.newChain(), src, true)
		second := chainDraw(d.newChain(), src, true)
		if !bytes.Equal(first.Pix, second.Pix) {
			t.Errorf("%s: two runs of the same chain differ", d.name)
		}
	}
}

// oobRecorder records out-of-bounds image accesses. It is safe for concurrent
// use by parallel filter workers.
type oobRecorder struct {
	mu       sync.Mutex
	readOOB  []image.Point
	writeOOB []image.Point
}

func (r *oobRecorder) recordRead(x, y int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.readOOB) < 8 {
		r.readOOB = append(r.readOOB, image.Pt(x, y))
	}
}

func (r *oobRecorder) recordWrite(x, y int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.writeOOB) < 8 {
		r.writeOOB = append(r.writeOOB, image.Pt(x, y))
	}
}

// boundedSrc wraps an image and records every read outside its bounds.
type boundedSrc struct {
	img *image.NRGBA
	rec *oobRecorder
}

func (p boundedSrc) ColorModel() color.Model { return p.img.ColorModel() }
func (p boundedSrc) Bounds() image.Rectangle { return p.img.Bounds() }
func (p boundedSrc) At(x, y int) color.Color {
	if !image.Pt(x, y).In(p.img.Bounds()) {
		p.rec.recordRead(x, y)
		return color.NRGBA{}
	}
	return p.img.At(x, y)
}

// boundedDst wraps a draw.Image and records every write outside its bounds.
type boundedDst struct {
	img *image.NRGBA
	rec *oobRecorder
}

func (p boundedDst) ColorModel() color.Model { return p.img.ColorModel() }
func (p boundedDst) Bounds() image.Rectangle { return p.img.Bounds() }
func (p boundedDst) At(x, y int) color.Color { return p.img.At(x, y) }
func (p boundedDst) Set(x, y int, c color.Color) {
	if !image.Pt(x, y).In(p.img.Bounds()) {
		p.rec.recordWrite(x, y)
		return
	}
	p.img.Set(x, y, c)
}

// TestChainBoundedRegionAccess verifies that region access inside a chain is
// bounded: no filter reads outside its input bounds or writes outside its
// output bounds, and the src image is never modified. It runs the matrix both
// with zero-Min and non-zero-Min source bounds.
func TestChainBoundedRegionAccess(t *testing.T) {
	srcBounds := []image.Rectangle{
		image.Rect(0, 0, 48, 36),
		image.Rect(-7, 5, 41, 41),
	}
	for _, sb := range srcBounds {
		for _, parallel := range []bool{true, false} {
			for _, d := range chainTestMatrix {
				name := fmt.Sprintf("%s/src=%v/parallel=%v", d.name, sb, parallel)

				src := chainTestSrc(sb)
				srcPix := make([]uint8, len(src.Pix))
				copy(srcPix, src.Pix)

				rec := &oobRecorder{}
				g := New(d.newChain()...)
				g.SetParallelization(parallel)

				dstPix := image.NewNRGBA(g.Bounds(src.Bounds()))
				g.Draw(boundedDst{dstPix, rec}, boundedSrc{src, rec})

				if len(rec.readOOB) > 0 {
					t.Errorf("%s: out-of-bounds reads, e.g. %v", name, rec.readOOB)
				}
				if len(rec.writeOOB) > 0 {
					t.Errorf("%s: out-of-bounds writes, e.g. %v", name, rec.writeOOB)
				}
				if !bytes.Equal(src.Pix, srcPix) {
					t.Errorf("%s: src image was modified", name)
				}
			}
		}
	}
}

// TestChainSingleFilterUnchanged verifies that a single filter executed
// through a GIFT chain behaves exactly as the bare filter applied directly.
func TestChainSingleFilterUnchanged(t *testing.T) {
	src := chainTestSrc(image.Rect(0, 0, 48, 36))
	for _, parallel := range []bool{true, false} {
		for _, d := range chainTestMatrix {
			filters := d.newChain()
			if len(filters) != 1 {
				continue
			}
			name := fmt.Sprintf("%s/parallel=%v", d.name, parallel)

			viaChain := chainDraw(d.newChain(), src, parallel)

			direct := image.NewNRGBA(viaChain.Bounds())
			filters[0].Draw(direct, src, &Options{Parallelization: parallel})

			if !bytes.Equal(viaChain.Pix, direct.Pix) {
				t.Errorf("%s: single filter in a chain differs from the bare filter", name)
			}
		}
	}
}
