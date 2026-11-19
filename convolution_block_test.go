package gift

import (
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math/rand"
	"runtime"
	"testing"
)

// referenceConvolutionDraw is a verbatim copy of the pre-optimization
// convolutionFilter.Draw implementation: it keeps ksize full-image-width
// rotating rows per worker. It is used as the ground truth to prove that the
// blocked implementation produces byte-identical output pixels.
func referenceConvolutionDraw(p *convolutionFilter, dst draw.Image, src image.Image, options *Options) {
	if options == nil {
		options = &defaultOptions
	}

	srcb := src.Bounds()
	dstb := dst.Bounds()

	if srcb.Dx() <= 0 || srcb.Dy() <= 0 {
		return
	}

	ksize, weights := prepareConvolutionWeights(p.kernel, p.normalize)
	kcenter := ksize / 2

	if ksize < 1 {
		copyimage(dst, src, options)
		return
	}

	pixGetter := newPixelGetter(src)
	pixSetter := newPixelSetter(dst)

	parallelize(options.Parallelization, srcb.Min.Y, srcb.Max.Y, func(start, stop int) {
		starty := start
		rows := make([][]pixel, ksize)
		for i := 0; i < ksize; i++ {
			rowy := starty + i - kcenter
			if rowy < srcb.Min.Y {
				rowy = srcb.Min.Y
			} else if rowy > srcb.Max.Y-1 {
				rowy = srcb.Max.Y - 1
			}
			row := make([]pixel, srcb.Dx())
			pixGetter.getPixelRow(rowy, &row)
			rows[i] = row
		}

		for y := start; y < stop; y++ {
			for x := srcb.Min.X; x < srcb.Max.X; x++ {
				var r, g, b, a float32
				for _, w := range weights {
					wx := x + w.u
					if wx < srcb.Min.X {
						wx = srcb.Min.X
					} else if wx > srcb.Max.X-1 {
						wx = srcb.Max.X - 1
					}
					rowsx := wx - srcb.Min.X
					rowsy := kcenter + w.v

					px := rows[rowsy][rowsx]
					r += px.r * w.weight
					g += px.g * w.weight
					b += px.b * w.weight
					if p.alpha {
						a += px.a * w.weight
					}
				}
				if p.abs {
					r = absf32(r)
					g = absf32(g)
					b = absf32(b)
					if p.alpha {
						a = absf32(a)
					}
				}
				if p.delta != 0 {
					r += p.delta
					g += p.delta
					b += p.delta
					if p.alpha {
						a += p.delta
					}
				}
				if !p.alpha {
					a = rows[kcenter][x-srcb.Min.X].a
				}
				pixSetter.setPixel(dstb.Min.X+x-srcb.Min.X, dstb.Min.Y+y-srcb.Min.Y, pixel{r, g, b, a})
			}

			if y < stop-1 {
				tmprow := rows[0]
				for i := 0; i < ksize-1; i++ {
					rows[i] = rows[i+1]
				}
				nextrowy := y + ksize/2 + 1
				if nextrowy > srcb.Max.Y-1 {
					nextrowy = srcb.Max.Y - 1
				}
				pixGetter.getPixelRow(nextrowy, &tmprow)
				rows[ksize-1] = tmprow
			}
		}
	})
}

func randomKernel(rng *rand.Rand, ksize int, normalize, abs bool) []float32 {
	kernel := make([]float32, ksize*ksize)
	for i := range kernel {
		// Mix positive, negative and occasionally zero weights, including
		// values large enough to clamp on output.
		v := rng.Float32()*2 - 1
		if rng.Intn(7) == 0 {
			v = 0
		}
		if abs {
			if v < 0 {
				v = -v
			}
		}
		kernel[i] = v * 3
	}
	if normalize && ksize > 1 {
		// Keep a non-zero sum so normalization does not bail out.
		kernel[ksize/2*ksize+ksize/2] += float32(ksize) * 2
	}
	return kernel
}

func pixHash(pi interface{ Bounds() image.Rectangle }, pix []byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "%v|", pi.Bounds())
	h.Write(pix)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// TestConvolutionBlockedMatchesReference proves output pixels of the blocked
// implementation are byte-identical to the original full-width-row
// implementation across image types, bounds offsets, kernel sizes and all
// filter parameter combinations, in both parallel and serial modes.
func TestConvolutionBlockedMatchesReference(t *testing.T) {
	const w, h = 333, 71
	rng := rand.New(rand.NewSource(20261119))

	srcNRGBA := image.NewNRGBA(image.Rect(-7, -3, -7+w, -3+h))
	for i := range srcNRGBA.Pix {
		srcNRGBA.Pix[i] = uint8(rng.Intn(256))
	}
	srcRGBA := image.NewRGBA(image.Rect(5, 9, 5+w, 9+h))
	copy(srcRGBA.Pix, srcNRGBA.Pix)
	srcGray := image.NewGray(image.Rect(0, 0, w, h))
	for i := range srcGray.Pix {
		srcGray.Pix[i] = uint8(rng.Intn(256))
	}
	palette := color.Palette{
		color.RGBA{0, 0, 0, 255},
		color.RGBA{255, 0, 0, 255},
		color.RGBA{0, 255, 0, 255},
		color.RGBA{0, 0, 255, 255},
		color.RGBA{128, 128, 128, 255},
		color.RGBA{255, 255, 255, 255},
	}
	srcPal := image.NewPaletted(image.Rect(-1, 2, -1+w, 2+h), palette)
	for i := range srcPal.Pix {
		srcPal.Pix[i] = uint8(rng.Intn(len(palette)))
	}

	srcs := map[string]image.Image{
		"NRGBA":    srcNRGBA,
		"RGBA":     srcRGBA,
		"Gray":     srcGray,
		"Paletted": srcPal,
	}

	kernelSizes := []int{1, 3, 7, 31, 65}
	type params struct {
		normalize, alpha, abs bool
		delta                 float32
	}
	paramSets := []params{
		{false, false, false, 0},
		{true, false, false, 0},
		{false, true, false, 0},
		{true, true, true, 0},
		{false, false, true, 0},
		{false, true, false, 5 / 255.0},
		{true, true, false, -3 / 255.0},
	}

	for srcName, src := range srcs {
		for _, ksize := range kernelSizes {
			for _, ps := range paramSets {
				kernel := randomKernel(rng, ksize, ps.normalize, ps.abs)
				for _, parallel := range []bool{true, false} {
					opts := &Options{Parallelization: parallel}
					f := Convolution(kernel, ps.normalize, ps.alpha, ps.abs, ps.delta)
					cf := f.(*convolutionFilter)

					dstGot := image.NewNRGBA(f.Bounds(src.Bounds()))
					cf.Draw(dstGot, src, opts)

					dstWant := image.NewNRGBA(f.Bounds(src.Bounds()))
					referenceConvolutionDraw(cf, dstWant, src, opts)

					hGot := pixHash(dstGot, dstGot.Pix)
					hWant := pixHash(dstWant, dstWant.Pix)
					if hGot != hWant {
						// Locate the first differing pixel for a useful report.
						first := -1
						for i := range dstGot.Pix {
							if dstGot.Pix[i] != dstWant.Pix[i] {
								first = i
								break
							}
						}
						t.Fatalf("pixel mismatch src=%s ksize=%d params=%+v parallel=%v firstDiffByte=%d got=%s want=%s",
							srcName, ksize, ps, parallel, first, hGot[:12], hWant[:12])
					}
				}
			}
		}
	}
}

// measureConvHeap runs fn twice and returns the heap bytes allocated during
// the second run (the warm run avoids one-time startup allocations).
func measureConvHeap(fn func()) uint64 {
	fn()
	runtime.GC()
	var m0, m1 runtime.MemStats
	runtime.ReadMemStats(&m0)
	fn()
	runtime.ReadMemStats(&m1)
	return m1.TotalAlloc - m0.TotalAlloc
}

// TestConvolutionBlockedMemoryBounded verifies with a large kernel on large
// images that scratch memory of the blocked implementation stays bounded by
// the block cap (independent of image width), while the original
// full-width-row implementation grows linearly with width.
func TestConvolutionBlockedMemoryBounded(t *testing.T) {
	const (
		ksize     = 101
		h         = 512
		widthSm   = 1024
		widthLg   = 4096
		blockRows = ksize
	)
	kernel := randomKernel(rand.New(rand.NewSource(424242)), ksize, true, false)

	run := func(width int, blocked bool) uint64 {
		src := image.NewNRGBA(image.Rect(0, 0, width, h))
		f := Convolution(kernel, true, true, false, 0).(*convolutionFilter)
		// Allocate the destination outside the measured closure so the number
		// reflects scratch memory (rotating row buffers and per-pixel access)
		// only, not the mandatory output image.
		dst := image.NewNRGBA(f.Bounds(src.Bounds()))
		return measureConvHeap(func() {
			if blocked {
				f.Draw(dst, src, &Options{Parallelization: true})
			} else {
				referenceConvolutionDraw(f, dst, src, &Options{Parallelization: true})
			}
		})
	}

	blockedSm := run(widthSm, true)
	blockedLg := run(widthLg, true)
	legacySm := run(widthSm, false)
	legacyLg := run(widthLg, false)

	t.Logf("scratch heap bytes: blocked w=%d -> %d, w=%d -> %d", widthSm, blockedSm, widthLg, blockedLg)
	t.Logf("scratch heap bytes: legacy  w=%d -> %d, w=%d -> %d", widthSm, legacySm, widthLg, legacyLg)
	t.Logf("per-worker row buffer cap = ksize*(blockWidth+ksize-1) pixels = %d",
		blockRows*(convolutionBlockWidth+ksize-1))

	// With the destination excluded, the blocked scratch memory must stay
	// effectively flat when the width grows 4x: rotating row buffers are
	// bounded by convolutionBlockWidth, independent of image width.
	if blockedLg > blockedSm*12/10 {
		t.Errorf("blocked scratch memory grows with image width: %d -> %d", blockedSm, blockedLg)
	}
	// The legacy implementation must clearly grow with width.
	if legacyLg <= legacySm*2 {
		t.Errorf("legacy reference unexpectedly flat: %d -> %d", legacySm, legacyLg)
	}
	// On the large image the blocked implementation must save a substantial
	// amount of heap allocation versus the original.
	if blockedLg >= legacyLg/2 {
		t.Errorf("blocked implementation does not halve allocations: blocked=%d legacy=%d", blockedLg, legacyLg)
	}
}

// TestConvolutionBlockedGoldenImage hashes the golden testdata image through
// the public API (large-kernel Mean filter routes via Convolution) and
// compares against the committed golden file, guarding end-to-end behavior.
func TestConvolutionBlockedGoldenImage(t *testing.T) {
	src := loadImage(t, "testdata/src.png")
	g := New(Mean(5, true))
	dst := image.NewNRGBA(g.Bounds(src.Bounds()))
	g.Draw(dst, src)
	want := loadImageNRGBA(t, "testdata/dst_mean.png")
	if !goldenEqual(dst, want) {
		t.Errorf("mean (convolution-based) output differs from golden image")
	}
}
