package gift

import (
	"image"
	"image/color"
	"image/draw"
)

// Range selects the coding range of luma (Y) and chroma (Cb, Cr) samples
// used by the RGB/YCbCr conversion filters.
type Range int

const (
	// FullRange is the full-range (a.k.a. JFIF) coding: Y, Cb and Cr span
	// the whole 0..255 interval, with neutral chroma at 128.
	FullRange Range = iota

	// VideoRange is the limited (video) range coding defined by ITU-R BT.601:
	// luma is mapped onto 16..235 and chroma onto 16..240, with neutral
	// chroma at 128.
	VideoRange
)

// clampu8 clips v to the unsigned 8-bit interval.
func clampu8(v int) uint8 {
	switch {
	case v < 0:
		return 0
	case v > 0xff:
		return 0xff
	default:
		return uint8(v)
	}
}

// scaleLimited maps a full-range excursion to a limited-range excursion with
// rounding to the nearest integer (halves away from zero). The same formula
// applies to luma (219/255 excursion) and chroma (224/255 excursion):
//
//	round(v * scale / 255) == (v*scale + sign(v)*127) / 255
func scaleLimited(v, scale int) int {
	if v >= 0 {
		return (v*scale + 127) / 255
	}
	return -((-v*scale + 127) / 255)
}

// Fused limited-range YCbCr -> RGB coefficients scaled by 1e6. They combine
// the limited->full range expansion (255/219 for luma, 255/224 for chroma)
// with the JFIF color matrix in a single rounding, e.g.
//
//	1.402 * 255/224 = 1.598170...  -> cRcrM
//	255/219         = 1.164384...  -> cYM
const (
	cYM   int64 = 1164384 // round(255/219 * 1e6)
	cRcrM int64 = 1598170 // round(1.402 * 255/224 * 1e6)
	cGcbM int64 = 391768  // round(0.344136 * 255/224 * 1e6)
	cGcrM int64 = 813210  // round(0.714136 * 255/224 * 1e6)
	cBcbM int64 = 2017412 // round(1.772 * 255/224 * 1e6)
)

// roundDivM rounds v/1e6 to the nearest integer, halves away from zero.
func roundDivM(v int64) int64 {
	if v >= 0 {
		return (v + 500000) / 1000000
	}
	return -((-v + 500000) / 1000000)
}

// RGBToYCbCr converts an 8-bit RGB color triplet to YCbCr using the given
// coding range. Rounding is performed to the nearest integer (halves away
// from zero) and the resulting samples are clamped to the legal code range:
// 0..255 for Y/Cb/Cr in full range, 16..235 for Y and 16..240 for Cb/Cr in
// video range. The full-range output is byte-identical to
// image/color.RGBToYCbCr.
func RGBToYCbCr(r, g, b uint8, rng Range) (y, cb, cr uint8) {
	// Full-range (JFIF) stage: reuse the standard library so the result is
	// guaranteed to match its integer pipeline exactly.
	yf, cbf, crf := color.RGBToYCbCr(r, g, b)

	if rng == VideoRange {
		// Full-range -> limited (video) range per ITU-R BT.601:
		// Y in 16..235, Cb/Cr in 16..240 (neutral 128).
		yv := 16 + scaleLimited(int(yf), 219)
		cbv := 128 + scaleLimited(int(cbf)-128, 224)
		crv := 128 + scaleLimited(int(crf)-128, 224)
		return clampu8Range(yv, 16, 235),
			clampu8Range(cbv, 16, 240),
			clampu8Range(crv, 16, 240)
	}

	return yf, cbf, crf
}

// YCbCrToRGB converts an 8-bit YCbCr color triplet to RGB using the given
// coding range. Rounding is performed to the nearest integer (halves away
// from zero) and the resulting samples are clamped to 0..255.
//
// In full range the conversion follows the JFIF integer pipeline and is
// byte-identical to image/color.YCbCrToRGB. In video range the limited
// ranges (Y 16..235, Cb/Cr 16..240) are decoded with the canonical
// ITU-R BT.601 formula evaluated in a single high-precision fixed-point
// step, so that a video-range round trip does not drift.
func YCbCrToRGB(y, cb, cr uint8, rng Range) (r, g, b uint8) {
	if rng == VideoRange {
		dy := int64(y) - 16
		dcb := int64(cb) - 128
		dcr := int64(cr) - 128

		rv := roundDivM(cYM*dy + cRcrM*dcr)
		gv := roundDivM(cYM*dy - cGcbM*dcb - cGcrM*dcr)
		bv := roundDivM(cYM*dy + cBcbM*dcb)

		return uint8(clampi64(rv, 0, 0xff)),
			uint8(clampi64(gv, 0, 0xff)),
			uint8(clampi64(bv, 0, 0xff))
	}

	return color.YCbCrToRGB(y, cb, cr)
}

func clampi64(v int64, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampu8Range(v, lo, hi int) uint8 {
	return uint8(clampi(v, lo, hi))
}

// encodeRGBToYCbCrPixel converts an RGB pixel (normalized 0..1 channels) to
// YCbCr samples stored back into the r/g/b channels (each holding the code
// value divided by 255). The alpha channel is preserved.
func encodeRGBToYCbCrPixel(px pixel, rng Range) pixel {
	r8 := f32u8(px.r * 0xff)
	g8 := f32u8(px.g * 0xff)
	b8 := f32u8(px.b * 0xff)
	y, cb, cr := RGBToYCbCr(r8, g8, b8, rng)
	return pixel{
		r: float32(y) * qf8,
		g: float32(cb) * qf8,
		b: float32(cr) * qf8,
		a: px.a,
	}
}

// decodeYCbCrPixelToRGB converts a pixel whose r/g/b channels carry YCbCr
// code values (normalized by 255) back into an RGB pixel. The alpha channel
// is preserved.
func decodeYCbCrPixelToRGB(px pixel, rng Range) pixel {
	y8 := f32u8(px.r * 0xff)
	cb8 := f32u8(px.g * 0xff)
	cr8 := f32u8(px.b * 0xff)
	r, g, b := YCbCrToRGB(y8, cb8, cr8, rng)
	return pixel{
		r: float32(r) * qf8,
		g: float32(g) * qf8,
		b: float32(b) * qf8,
		a: px.a,
	}
}

// ycbcrCOffset returns the offset of the chroma sample associated with the
// given pixel for the image's subsampling ratio. It mirrors the indexing used
// by pixelGetter.getPixel for *image.YCbCr sources.
func ycbcrCOffset(img *image.YCbCr, x, y int) int {
	switch img.SubsampleRatio {
	case image.YCbCrSubsampleRatio444:
		return (y-img.Rect.Min.Y)*img.CStride + (x - img.Rect.Min.X)
	case image.YCbCrSubsampleRatio422:
		return (y-img.Rect.Min.Y)*img.CStride + (x/2 - img.Rect.Min.X/2)
	case image.YCbCrSubsampleRatio420:
		return (y/2-img.Rect.Min.Y/2)*img.CStride + (x/2 - img.Rect.Min.X/2)
	case image.YCbCrSubsampleRatio440:
		return (y/2-img.Rect.Min.Y/2)*img.CStride + (x - img.Rect.Min.X)
	default:
		return img.COffset(x, y)
	}
}

// rgbToYCbCrFilter converts RGB images into YCbCr-coded images.
//
// The YCbCr samples are encoded into the destination's r/g/b channels (each
// holding a code value normalized by 255) so that the conversion can be
// combined with any other filter in a chain; a later YCbCrToRGBFilter
// decodes them. The coding range is supplied explicitly and carried by the
// filter itself, so full-range and video-range stages never get mixed
// silently. To materialize a real *image.YCbCr use DrawRGBToYCbCr.
type rgbToYCbCrFilter struct {
	rng Range
}

// RGBToYCbCrFilter creates a filter that converts an image from RGB to YCbCr
// using the given coding range. The resulting image keeps the YCbCr samples
// in its red, green and blue channels respectively.
func RGBToYCbCrFilter(rng Range) Filter {
	return &rgbToYCbCrFilter{rng: rng}
}

func (f *rgbToYCbCrFilter) Bounds(srcBounds image.Rectangle) image.Rectangle {
	return image.Rect(0, 0, srcBounds.Dx(), srcBounds.Dy())
}

func (f *rgbToYCbCrFilter) Draw(dst draw.Image, src image.Image, options *Options) {
	if options == nil {
		options = &defaultOptions
	}

	srcb := src.Bounds()
	dstb := dst.Bounds()
	pg := newPixelGetter(src)
	ps := newPixelSetter(dst)
	rng := f.rng

	parallelize(options.Parallelization, srcb.Min.Y, srcb.Max.Y, func(start, stop int) {
		for y := start; y < stop; y++ {
			for x := srcb.Min.X; x < srcb.Max.X; x++ {
				px := encodeRGBToYCbCrPixel(pg.getPixel(x, y), rng)
				ps.setPixel(dstb.Min.X+x-srcb.Min.X, dstb.Min.Y+y-srcb.Min.Y, px)
			}
		}
	})
}

// DrawRGBToYCbCr converts an RGB image into a real *image.YCbCr and writes
// the Y/Cb/Cr planes of dst. dst must have the same size as src. Subsampled
// chroma planes receive the rounded average of all pixels of the
// corresponding chroma block. It is a convenience wrapper around
// RGBToYCbCrFilter for callers that need planar YCbCr output, since
// *image.YCbCr is not a draw.Image and cannot be used as a filter
// destination directly.
func DrawRGBToYCbCr(dst *image.YCbCr, src image.Image, rng Range, options *Options) {
	if options == nil {
		options = &defaultOptions
	}

	srcb := src.Bounds()
	pg := newPixelGetter(src)
	w, h := srcb.Dx(), srcb.Dy()

	// Chroma block dimensions in full-resolution pixels.
	bw, bh := 1, 1
	switch dst.SubsampleRatio {
	case image.YCbCrSubsampleRatio422:
		bw = 2
	case image.YCbCrSubsampleRatio420, image.YCbCrSubsampleRatio440:
		bw, bh = 2, 2
	}

	parallelize(options.Parallelization, 0, h, func(start, stop int) {
		// Luma, one sample per pixel.
		for ly := start; ly < stop; ly++ {
			for lx := 0; lx < w; lx++ {
				px := pg.getPixel(srcb.Min.X+lx, srcb.Min.Y+ly)
				y, _, _ := RGBToYCbCr(f32u8(px.r*0xff), f32u8(px.g*0xff), f32u8(px.b*0xff), rng)
				dst.Y[ly*dst.YStride+lx] = y
			}
		}

		// Chroma blocks intersecting this strip.
		for by := (start / bh) * bh; by < stop; by += bh {
			for bx := 0; bx < w; bx += bw {
				var sumCb, sumCr, n int
				y0 := by
				y1 := by + bh
				if y1 > h {
					y1 = h
				}
				x1 := bx + bw
				if x1 > w {
					x1 = w
				}
				for ly := y0; ly < y1; ly++ {
					for lx := bx; lx < x1; lx++ {
						px := pg.getPixel(srcb.Min.X+lx, srcb.Min.Y+ly)
						_, cb, cr := RGBToYCbCr(f32u8(px.r*0xff), f32u8(px.g*0xff), f32u8(px.b*0xff), rng)
						sumCb += int(cb)
						sumCr += int(cr)
						n++
					}
				}
				ic := ycbcrCOffset(dst, srcb.Min.X+bx, srcb.Min.Y+by)
				dst.Cb[ic] = clampu8((sumCb + n/2) / n)
				dst.Cr[ic] = clampu8((sumCr + n/2) / n)
			}
		}
	})
}

// yCbCrToRGBFilter converts YCbCr images into RGB.
//
// An *image.YCbCr source is read plane by plane honoring its subsampling
// ratio. Any other source is treated as an image produced by an
// RGBToYCbCrFilter (YCbCr samples carried in the r/g/b channels). The coding
// range is taken from the filter, making a conversion chain unambiguous.
type yCbCrToRGBFilter struct {
	rng Range
}

// YCbCrToRGBFilter creates a filter that converts an image from YCbCr to RGB
// using the given coding range.
func YCbCrToRGBFilter(rng Range) Filter {
	return &yCbCrToRGBFilter{rng: rng}
}

func (f *yCbCrToRGBFilter) Bounds(srcBounds image.Rectangle) image.Rectangle {
	return image.Rect(0, 0, srcBounds.Dx(), srcBounds.Dy())
}

func (f *yCbCrToRGBFilter) Draw(dst draw.Image, src image.Image, options *Options) {
	if options == nil {
		options = &defaultOptions
	}

	srcb := src.Bounds()
	dstb := dst.Bounds()
	ps := newPixelSetter(dst)
	rng := f.rng

	if yimg, ok := src.(*image.YCbCr); ok {
		parallelize(options.Parallelization, srcb.Min.Y, srcb.Max.Y, func(start, stop int) {
			for y := start; y < stop; y++ {
				for x := srcb.Min.X; x < srcb.Max.X; x++ {
					iy := (y-srcb.Min.Y)*yimg.YStride + (x - srcb.Min.X)
					ic := ycbcrCOffset(yimg, x, y)
					r, g, b := YCbCrToRGB(yimg.Y[iy], yimg.Cb[ic], yimg.Cr[ic], rng)
					ps.setPixel(
						dstb.Min.X+x-srcb.Min.X,
						dstb.Min.Y+y-srcb.Min.Y,
						pixel{float32(r) * qf8, float32(g) * qf8, float32(b) * qf8, 1},
					)
				}
			}
		})
		return
	}

	pg := newPixelGetter(src)
	parallelize(options.Parallelization, srcb.Min.Y, srcb.Max.Y, func(start, stop int) {
		for y := start; y < stop; y++ {
			for x := srcb.Min.X; x < srcb.Max.X; x++ {
				px := decodeYCbCrPixelToRGB(pg.getPixel(x, y), rng)
				ps.setPixel(dstb.Min.X+x-srcb.Min.X, dstb.Min.Y+y-srcb.Min.Y, px)
			}
		}
	})
}
