package gift

import (
	"image"
	"image/draw"
)

// BlendMode is a blend mode used by the Blend filter to composite
// a source (top) layer over a destination (backdrop) image.
//
// The blend modes follow the W3C "Compositing and Blending Level 1"
// specification. The source layer is always composited over the backdrop
// using the source-over alpha compositing operator.
type BlendMode int

// Blend modes.
const (
	// Normal blend mode: the source color is placed over the backdrop
	// (plain source-over alpha compositing).
	Normal BlendMode = iota
	// Multiply blend mode: the source and backdrop colors are multiplied.
	Multiply
	// Screen blend mode: the inverse of the source and backdrop colors
	// are multiplied and the result is inverted.
	Screen
	// Overlay blend mode: Multiply or Screen depending on the backdrop color.
	Overlay
	// Darken blend mode: the darker of the source and backdrop colors.
	Darken
	// Lighten blend mode: the lighter of the source and backdrop colors.
	Lighten
	// ColorDodge blend mode: the backdrop color is brightened to reflect
	// the source color.
	ColorDodge
	// ColorBurn blend mode: the backdrop color is darkened to reflect
	// the source color.
	ColorBurn
	// HardLight blend mode: Multiply or Screen depending on the source color.
	HardLight
	// SoftLight blend mode: similar to HardLight, but softer.
	SoftLight
	// Difference blend mode: the absolute difference between the source
	// and backdrop colors.
	Difference
	// Exclusion blend mode: similar to Difference, but with lower contrast.
	Exclusion
	// Add blend mode: the source and backdrop colors are added.
	Add
	// Subtract blend mode: the source color is subtracted from the
	// backdrop color.
	Subtract
)

// blendChannel applies the blend mode function B(cb, cs) to a single
// color channel. Both cb (backdrop) and cs (source) are straight
// (non-premultiplied) color values in the range [0, 1].
func blendChannel(mode BlendMode, cb, cs float32) float32 {
	switch mode {
	case Multiply:
		return cb * cs
	case Screen:
		return cb + cs - cb*cs
	case Overlay:
		return hardLightChannel(cs, cb)
	case Darken:
		return minf32(cb, cs)
	case Lighten:
		return maxf32(cb, cs)
	case ColorDodge:
		if cb <= 0 {
			return 0
		}
		if cs >= 1 {
			return 1
		}
		return minf32(1, cb/(1-cs))
	case ColorBurn:
		if cb >= 1 {
			return 1
		}
		if cs <= 0 {
			return 0
		}
		return 1 - minf32(1, (1-cb)/cs)
	case HardLight:
		return hardLightChannel(cb, cs)
	case SoftLight:
		return softLightChannel(cb, cs)
	case Difference:
		return absf32(cb - cs)
	case Exclusion:
		return cb + cs - 2*cb*cs
	case Add:
		return minf32(1, cb+cs)
	case Subtract:
		return maxf32(0, cb-cs)
	default: // Normal
		return cs
	}
}

func hardLightChannel(cb, cs float32) float32 {
	if cs <= 0.5 {
		return 2 * cb * cs
	}
	return 1 - 2*(1-cb)*(1-cs)
}

func softLightChannel(cb, cs float32) float32 {
	if cs <= 0.5 {
		return cb - (1-2*cs)*cb*(1-cb)
	}
	var d float32
	if cb <= 0.25 {
		d = ((16*cb-12)*cb + 4) * cb
	} else {
		d = sqrtf32(cb)
	}
	return cb + (2*cs-1)*(d-cb)
}

// blendPixel composites the source pixel ps over the backdrop pixel pb
// using the given blend mode and returns the resulting pixel.
//
// Both the input and the output pixels use straight (non-premultiplied)
// alpha. Internally the compositing is performed in premultiplied form:
//
//	ao = as + ab*(1-as)
//	co = (as*cs*(1-ab) + ab*cb*(1-as) + as*ab*B(cb, cs)) / ao
//
// where B is the blend mode function applied to straight colors.
// A fully transparent source pixel leaves the backdrop untouched and
// a fully transparent backdrop lets the source pass through, so the
// colors of transparent pixels never bleed into visible pixels. If the
// resulting pixel is fully transparent, its color is zeroed.
func blendPixel(mode BlendMode, pb, ps pixel) pixel {
	as := ps.a
	ab := pb.a
	ao := as + ab*(1-as)
	if ao <= 0 {
		return pixel{0, 0, 0, 0}
	}
	if as <= 0 {
		return pb
	}
	if ab <= 0 {
		return ps
	}
	k1 := as * (1 - ab)
	k2 := ab * (1 - as)
	k3 := as * ab
	r := (k1*ps.r + k2*pb.r + k3*blendChannel(mode, pb.r, ps.r)) / ao
	g := (k1*ps.g + k2*pb.g + k3*blendChannel(mode, pb.g, ps.g)) / ao
	b := (k1*ps.b + k2*pb.b + k3*blendChannel(mode, pb.b, ps.b)) / ao
	return pixel{r, g, b, ao}
}

type blendFilter struct {
	img  image.Image
	mode BlendMode
}

func (p *blendFilter) Bounds(srcBounds image.Rectangle) (dstBounds image.Rectangle) {
	dstBounds = image.Rect(0, 0, srcBounds.Dx(), srcBounds.Dy())
	return
}

func (p *blendFilter) Draw(dst draw.Image, src image.Image, options *Options) {
	if options == nil {
		options = &defaultOptions
	}

	srcb := src.Bounds()
	dstb := dst.Bounds()
	imgb := p.img.Bounds()

	pixGetterSrc := newPixelGetter(src)
	pixGetterImg := newPixelGetter(p.img)
	pixSetter := newPixelSetter(dst)

	// The region of dst covered by src.
	r := srcb.Sub(srcb.Min).Add(dstb.Min).Intersect(dstb)

	parallelize(options.Parallelization, r.Min.Y, r.Max.Y, func(start, stop int) {
		for y := start; y < stop; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				pb := pixGetterSrc.getPixel(x-dstb.Min.X+srcb.Min.X, y-dstb.Min.Y+srcb.Min.Y)
				var ps pixel
				ix := x - dstb.Min.X + imgb.Min.X
				iy := y - dstb.Min.Y + imgb.Min.Y
				if image.Pt(ix, iy).In(imgb) {
					ps = pixGetterImg.getPixel(ix, iy)
				}
				pixSetter.setPixel(x, y, blendPixel(p.mode, pb, ps))
			}
		}
	})
}

// Blend creates a filter that composites the img image (the top layer)
// over the input image (the backdrop) using the specified blend mode.
//
// The top-left corner (Min point) of img is aligned with the top-left
// corner of the destination image. Pixels of the input image that are
// not covered by img are left unchanged.
//
// Both images are interpreted as straight (non-premultiplied) alpha
// images; premultiplied images (such as *image.RGBA) are converted
// automatically. The result has the same size as the input image.
//
// Example:
//
//	// Blend layer over src using the Multiply blend mode.
//	g := gift.New(gift.Blend(layer, gift.Multiply))
//	dst := image.NewRGBA(g.Bounds(src.Bounds()))
//	g.Draw(dst, src)
func Blend(img image.Image, mode BlendMode) Filter {
	return &blendFilter{
		img:  img,
		mode: mode,
	}
}
