/*
Package gift provides a set of useful image processing filters.

Basic usage:

	// 1. Create a new filter list and add some filters.
	g := gift.New(
	    gift.Resize(800, 0, gift.LanczosResampling),
	    gift.UnsharpMask(1, 1, 0),
	)

	// 2. Create a new image of the corresponding size.
	// dst is a new target image, src is the original image.
	dst := image.NewRGBA(g.Bounds(src.Bounds()))

	// 3. Use the Draw func to apply the filters to src and store the result in dst.
	g.Draw(dst, src)

Filter chain semantics

A GIFT filter chain provides the following guarantees:

  - Execution order: filters are applied strictly in list order. The output of
    filter i is the input of filter i+1. Filters are never reordered, merged
    or skipped, so reordering the list may change the result (filters do not
    commute in general) and always does so observably.
  - Region bounds: every intermediate result is stored in a freshly allocated
    temporary image whose bounds are exactly the Bounds of the corresponding
    filter output. Chain stages never share or reuse pixel buffers, and the
    src image is never modified. Each filter reads only pixels inside its
    input bounds and writes only pixels inside its output bounds.
  - Reproducibility: applying the same filter list to the same src image
    always produces exactly the same dst pixels. In particular, enabling or
    disabling parallelization (see SetParallelization) never changes the
    resulting pixels.

These guarantees are verified by the fixed chain matrix in chain_test.go.

*/
package gift

import (
	"image"
	"image/draw"
)

// Filter is an image processing filter.
type Filter interface {
	// Draw applies the filter to the src image and outputs the result to the dst image.
	Draw(dst draw.Image, src image.Image, options *Options)
	// Bounds calculates the appropriate bounds of an image after applying the filter.
	Bounds(srcBounds image.Rectangle) (dstBounds image.Rectangle)
}

// Options is the parameters passed to image processing filters.
type Options struct {
	// Parallelization enables parallel processing of image rows.
	// It affects performance only: the resulting pixels are exactly
	// the same whether parallelization is enabled or not.
	Parallelization bool
}

var defaultOptions = Options{
	Parallelization: true,
}

// GIFT is a list of image processing filters.
type GIFT struct {
	Filters []Filter
	Options Options
}

// New creates a new filter list and initializes it with the given slice of filters.
func New(filters ...Filter) *GIFT {
	return &GIFT{
		Filters: filters,
		Options: defaultOptions,
	}
}

// SetParallelization enables or disables the image processing parallelization.
// Parallelization is enabled by default. The setting affects performance only;
// the resulting pixels are identical either way.
func (g *GIFT) SetParallelization(isEnabled bool) {
	g.Options.Parallelization = isEnabled
}

// Parallelization returns the current state of parallelization option.
func (g *GIFT) Parallelization() bool {
	return g.Options.Parallelization
}

// Add appends the given filters to the list of filters.
func (g *GIFT) Add(filters ...Filter) {
	g.Filters = append(g.Filters, filters...)
}

// Empty removes all the filters from the list.
func (g *GIFT) Empty() {
	g.Filters = []Filter{}
}

// Bounds calculates the appropriate bounds for the result image after applying all the added filters.
// Parameter srcBounds is the bounds of the source image.
//
// Example:
//
// 	src := image.NewRGBA(image.Rect(0, 0, 100, 200))
//	g := gift.New(gift.Rotate90())
//
// 	// calculate image bounds after applying rotation and create a new image of that size.
// 	dst := image.NewRGBA(g.Bounds(src.Bounds())) // dst bounds: (0, 0, 200, 100)
//
//
func (g *GIFT) Bounds(srcBounds image.Rectangle) (dstBounds image.Rectangle) {
	b := srcBounds
	for _, f := range g.Filters {
		b = f.Bounds(b)
	}
	dstBounds = b
	return
}

// Draw applies all the added filters to the src image and outputs the result to the dst image.
//
// The filters are applied strictly in list order: the first added filter reads
// from src, each subsequent filter reads the output of the previous one, and
// the last filter writes to dst. Intermediate results are stored in freshly
// allocated temporary images, so src is never modified and no pixel buffer is
// shared between chain stages. The result is deterministic and identical
// regardless of the parallelization setting. See the package documentation
// for the full list of filter chain guarantees.
func (g *GIFT) Draw(dst draw.Image, src image.Image) {
	if len(g.Filters) == 0 {
		copyimage(dst, src, &g.Options)
		return
	}

	first, last := 0, len(g.Filters)-1
	var tmpIn image.Image
	var tmpOut draw.Image

	for i, f := range g.Filters {
		if i == first {
			tmpIn = src
		} else {
			tmpIn = tmpOut
		}

		if i == last {
			tmpOut = dst
		} else {
			tmpOut = createTempImage(f.Bounds(tmpIn.Bounds()))
		}

		f.Draw(tmpOut, tmpIn, &g.Options)
	}
}

// Operator is an image composition operator.
type Operator int

// Composition operators.
const (
	CopyOperator Operator = iota
	OverOperator
)

// DrawAt applies all the added filters to the src image and outputs the result to the dst image
// at the specified position pt using the specified composition operator op.
func (g *GIFT) DrawAt(dst draw.Image, src image.Image, pt image.Point, op Operator) {
	switch op {
	case OverOperator:
		tb := g.Bounds(src.Bounds())
		tb = tb.Sub(tb.Min).Add(pt)
		tmp := createTempImage(tb)
		g.Draw(tmp, src)
		pixGetterDst := newPixelGetter(dst)
		pixGetterTmp := newPixelGetter(tmp)
		pixSetterDst := newPixelSetter(dst)
		ib := tb.Intersect(dst.Bounds())
		parallelize(g.Options.Parallelization, ib.Min.Y, ib.Max.Y, func(start, stop int) {
			for y := start; y < stop; y++ {
				for x := ib.Min.X; x < ib.Max.X; x++ {
					px0 := pixGetterDst.getPixel(x, y)
					px1 := pixGetterTmp.getPixel(x, y)
					c1 := px1.a
					c0 := (1 - c1) * px0.a
					cs := c0 + c1
					c0 /= cs
					c1 /= cs
					r := px0.r*c0 + px1.r*c1
					g := px0.g*c0 + px1.g*c1
					b := px0.b*c0 + px1.b*c1
					a := px0.a + px1.a*(1-px0.a)
					pixSetterDst.setPixel(x, y, pixel{r, g, b, a})
				}
			}
		})

	default:
		if pt.Eq(dst.Bounds().Min) {
			g.Draw(dst, src)
			return
		}
		if subimg, ok := getSubImage(dst, pt); ok {
			g.Draw(subimg, src)
			return
		}
		tb := g.Bounds(src.Bounds())
		tb = tb.Sub(tb.Min).Add(pt)
		tmp := createTempImage(tb)
		g.Draw(tmp, src)
		pixGetter := newPixelGetter(tmp)
		pixSetter := newPixelSetter(dst)
		ib := tb.Intersect(dst.Bounds())
		parallelize(g.Options.Parallelization, ib.Min.Y, ib.Max.Y, func(start, stop int) {
			for y := start; y < stop; y++ {
				for x := ib.Min.X; x < ib.Max.X; x++ {
					pixSetter.setPixel(x, y, pixGetter.getPixel(x, y))
				}
			}
		})
	}
}

func getSubImage(img draw.Image, pt image.Point) (draw.Image, bool) {
	if !pt.In(img.Bounds()) {
		return nil, false
	}
	switch img := img.(type) {
	case *image.Gray:
		return img.SubImage(image.Rectangle{pt, img.Bounds().Max}).(draw.Image), true
	case *image.Gray16:
		return img.SubImage(image.Rectangle{pt, img.Bounds().Max}).(draw.Image), true
	case *image.RGBA:
		return img.SubImage(image.Rectangle{pt, img.Bounds().Max}).(draw.Image), true
	case *image.RGBA64:
		return img.SubImage(image.Rectangle{pt, img.Bounds().Max}).(draw.Image), true
	case *image.NRGBA:
		return img.SubImage(image.Rectangle{pt, img.Bounds().Max}).(draw.Image), true
	case *image.NRGBA64:
		return img.SubImage(image.Rectangle{pt, img.Bounds().Max}).(draw.Image), true
	default:
		return nil, false
	}
}
