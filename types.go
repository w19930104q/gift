package gift

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"reflect"
)

// This file describes the image types that can flow through a filter chain,
// documents their color and alpha semantics, and provides validation helpers
// that turn otherwise opaque nil-pointer panics into readable errors.
//
// # Pipeline type model
//
// Every filter reads its source through a pixelGetter, which normalizes all
// supported image types into a single internal representation:
//
//	pixel{r, g, b, a float32}
//
// where each component is in the range [0, 1]. The RGB components are stored
// as non-premultiplied ("straight") colors: r, g and b are the color of the
// pixel and a is its opacity, independently. This matches the semantics of
// image.NRGBA and image.NRGBA64.
//
// Conversions into the internal representation follow these rules:
//
//   - image.NRGBA / image.NRGBA64 (8-bit and 16-bit straight alpha):
//     channel values are scaled by 1/255 or 1/65535 respectively; no color
//     transformation is applied and the alpha channel is preserved.
//   - image.RGBA / image.RGBA64 (8-bit and 16-bit premultiplied alpha):
//     colors are un-premultiplied into straight alpha (r = Ra/A, and so on).
//     Fully transparent pixels (a == 0) become {0, 0, 0, 0} and fully opaque
//     pixels keep a == 1.
//   - image.Gray / image.Gray16: the gray value is replicated onto r, g and b
//     and the pixel is treated as fully opaque (a == 1). Gray images have no
//     alpha channel.
//   - image.YCbCr (for example JPEG output): YCbCr is converted to RGB using
//     the library's fixed integer arithmetic and the pixel is treated as fully
//     opaque (a == 1). YCbCr is not a draw.Image, so it can only be used as a
//     source image, never as a destination.
//   - image.Paletted: each index is resolved through the palette; the palette
//     colors are converted the same way as their underlying color types.
//   - Any other image.Image ("generic" images, for example image.Alpha or
//     image.Uniform): pixels are read through the generic color.Color API and
//     quantized to 16-bit precision (the color.Color RGBA() contract returns
//     16-bit values).
//
// Between two filters the chain stores intermediate results in 16-bit
// straight-alpha images (image.NRGBA64, see createTempImage). Therefore an
// 8-bit source keeps only 8 bits of information per channel on the first read,
// while 16-bit sources retain their full bit depth across the whole chain.
//
// Conversions out of the internal representation (pixelSetter) follow these
// rules:
//
//   - NRGBA/NRGBA64 destinations: straight alpha is written directly and
//     values are rounded to the destination bit depth.
//   - RGBA/RGBA64 destinations: straight colors are premultiplied
//     (Ra = r*a, and so on) before writing.
//   - Gray/Gray16 destinations: luminance is computed as
//     0.299*r + 0.587*g + 0.114*b, multiplied by alpha, and rounded to the
//     destination bit depth.
//   - Paletted destinations: the closest palette entry is selected by squared
//     Euclidean distance over r, g, b and a.
//   - Generic draw.Image destinations: pixels are written as color.NRGBA64,
//     i.e. rounded to 16 bits per channel.
//
// All built-in filters accept every readable source type and every writable
// destination type listed above; the type-specific behavior is centralized in
// the pixelGetter/pixelSetter conversions rather than in the filters.

// imageKind identifies the layout and color model of an image.
type imageKind int

const (
	kindUnknown imageKind = iota
	kindNRGBA
	kindNRGBA64
	kindRGBA
	kindRGBA64
	kindGray
	kindGray16
	kindYCbCr
	kindPaletted
	kindGeneric
)

// String returns the human-readable name of the image kind. It is used in
// validation error messages.
func (k imageKind) String() string {
	switch k {
	case kindNRGBA:
		return "*image.NRGBA"
	case kindNRGBA64:
		return "*image.NRGBA64"
	case kindRGBA:
		return "*image.RGBA"
	case kindRGBA64:
		return "*image.RGBA64"
	case kindGray:
		return "*image.Gray"
	case kindGray16:
		return "*image.Gray16"
	case kindYCbCr:
		return "*image.YCbCr"
	case kindPaletted:
		return "*image.Paletted"
	case kindGeneric:
		return "generic image.Image"
	default:
		return "unknown image type"
	}
}

// bitDepth is the number of bits per channel an image kind stores.
type bitDepth int

const (
	bitDepth8  bitDepth = 8
	bitDepth16 bitDepth = 16
)

// alphaMode describes how an image kind stores its alpha channel.
type alphaMode int

const (
	// alphaNone means the image kind has no alpha channel (fully opaque).
	alphaNone alphaMode = iota
	// alphaStraight means non-premultiplied alpha (NRGBA layout).
	alphaStraight
	// alphaPremultiplied means premultiplied alpha (RGBA layout).
	alphaPremultiplied
	// alphaPalette means the alpha is carried by the palette entries.
	alphaPalette
	// alphaGeneric means alpha semantics are defined by the image's
	// color.Color implementation.
	alphaGeneric
)

// imageInfo describes the type-level properties of an image. It carries no
// pixel data and can be computed without allocating or decoding anything.
type imageInfo struct {
	// kind is the concrete image layout.
	kind imageKind
	// name is the concrete Go type name, used in error messages.
	name string
	// depth is the per-channel bit depth of the storage.
	depth bitDepth
	// alpha describes how the alpha channel is stored.
	alpha alphaMode
	// readable reports whether the image implements image.Image, which every
	// non-nil image does. It is false for nil interface values.
	readable bool
	// writable reports whether the image implements draw.Image and can be used
	// as a filter destination.
	writable bool
}

// hasAlpha reports whether the image kind carries an alpha channel.
func (i imageInfo) hasAlpha() bool {
	return i.alpha != alphaNone
}

// is16Bit reports whether the image kind stores 16 bits per channel.
func (i imageInfo) is16Bit() bool {
	return i.depth == bitDepth16
}

// inspectImage classifies an image.Image without touching its pixels. A nil
// interface value or a typed nil pointer yields an imageInfo with readable set
// to false instead of panicking.
func inspectImage(img image.Image) imageInfo {
	if isNilInterface(img) {
		return imageInfo{kind: kindUnknown, name: "<nil>", readable: false, writable: false}
	}
	name := reflect.TypeOf(img).String()
	switch img.(type) {
	case *image.NRGBA:
		return imageInfo{kind: kindNRGBA, name: name, depth: bitDepth8, alpha: alphaStraight, readable: true, writable: true}
	case *image.NRGBA64:
		return imageInfo{kind: kindNRGBA64, name: name, depth: bitDepth16, alpha: alphaStraight, readable: true, writable: true}
	case *image.RGBA:
		return imageInfo{kind: kindRGBA, name: name, depth: bitDepth8, alpha: alphaPremultiplied, readable: true, writable: true}
	case *image.RGBA64:
		return imageInfo{kind: kindRGBA64, name: name, depth: bitDepth16, alpha: alphaPremultiplied, readable: true, writable: true}
	case *image.Gray:
		return imageInfo{kind: kindGray, name: name, depth: bitDepth8, alpha: alphaNone, readable: true, writable: true}
	case *image.Gray16:
		return imageInfo{kind: kindGray16, name: name, depth: bitDepth16, alpha: alphaNone, readable: true, writable: true}
	case *image.YCbCr:
		return imageInfo{kind: kindYCbCr, name: name, depth: bitDepth8, alpha: alphaNone, readable: true, writable: false}
	case *image.Paletted:
		return imageInfo{kind: kindPaletted, name: name, depth: bitDepth8, alpha: alphaPalette, readable: true, writable: true}
	default:
		_, writable := img.(draw.Image)
		return imageInfo{kind: kindGeneric, name: name, depth: bitDepth16, alpha: alphaGeneric, readable: true, writable: writable}
	}
}

// inspectDrawImage classifies a draw.Image. It additionally reports
// writability, which only a draw.Image can provide.
func inspectDrawImage(dst draw.Image) imageInfo {
	if isNilInterface(dst) {
		return imageInfo{kind: kindUnknown, name: "<nil>", readable: false, writable: false}
	}
	info := inspectImage(dst)
	info.writable = true
	return info
}

// isNilInterface reports whether v is a nil interface or holds a typed nil
// pointer. A typed nil (for example (*image.NRGBA)(nil) stored in an
// image.Image) would otherwise pass a plain nil check and panic on first use.
func isNilInterface(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// Sentinel errors describing the supported failure categories. Use errors.Is
// to test for them.
var (
	// ErrNilImage is returned when a source or destination image is nil.
	ErrNilImage = errors.New("gift: nil image")
	// ErrNilFilter is returned when the filter chain contains a nil filter.
	ErrNilFilter = errors.New("gift: nil filter")
	// ErrUnsupportedType is returned when an image type does not satisfy the
	// requirements of a filter or of its position in the chain (for example a
	// non-writable destination).
	ErrUnsupportedType = errors.New("gift: unsupported image type")
	// ErrInvalidFilter is returned when a filter is constructed or used with
	// invalid parameters (for example a nil resampling).
	ErrInvalidFilter = errors.New("gift: invalid filter")
)

// FilterError wraps a validation failure with the position of the failing
// filter inside the chain and the concrete types involved. It implements
// Unwrap so errors.Is works against the sentinel errors above.
type FilterError struct {
	// FilterIndex is the zero-based index of the failing filter, or -1 when
	// the failure is not tied to a particular filter (for example a nil
	// source image).
	FilterIndex int
	// FilterName is the concrete type name of the failing filter.
	FilterName string
	// Source describes the input image type at the failure point.
	Source imageInfo
	// Destination describes the output image type at the failure point when
	// relevant; otherwise it holds the zero value.
	Destination imageInfo
	// hasDestination reports whether Destination is meaningful.
	hasDestination bool
	// Op is a short verb describing the failing operation, for example
	// "validate source", "validate destination".
	Op string
	// Err is the wrapped sentinel error.
	Err error
}

func (e *FilterError) Error() string {
	where := "gift"
	if e.FilterIndex >= 0 {
		where = fmt.Sprintf("gift filter #%d (%s)", e.FilterIndex, e.FilterName)
	}
	switch {
	case errors.Is(e.Err, ErrNilImage):
		if e.Op == "validate destination" {
			return fmt.Sprintf("%s: destination image is nil", where)
		}
		return fmt.Sprintf("%s: source image is nil", where)
	case errors.Is(e.Err, ErrNilFilter):
		return fmt.Sprintf("%s: nil filter at chain index %d", where, e.FilterIndex)
	case errors.Is(e.Err, ErrUnsupportedType):
		if e.hasDestination {
			return fmt.Sprintf("%s: unsupported image type during %s: source=%s, destination=%s",
				where, e.Op, e.Source.name, e.Destination.name)
		}
		return fmt.Sprintf("%s: unsupported image type during %s: image=%s",
			where, e.Op, e.Source.name)
	default:
		return fmt.Sprintf("%s: %v (during %s)", where, e.Err, e.Op)
	}
}

// Unwrap exposes the wrapped sentinel for errors.Is/errors.As.
func (e *FilterError) Unwrap() error {
	return e.Err
}

// filterValidator is implemented by filters that can validate their inputs and
// parameters before touching any pixels. Built-in filters with extra
// construction parameters (Resize, Rotate) implement it; third-party filters
// may implement it to participate in Check and DrawChecked.
//
// src describes the actual input image type and dst describes the output
// image type when the filter is the last one in the chain. For intermediate
// filters dst describes the chain's internal 16-bit temporary image kind
// (kindNRGBA64).
type filterValidator interface {
	validate(src, dst imageInfo) error
}

// defaultFilterValidator is embedded by built-in filters that have no
// type-specific restrictions: every readable source and every writable
// destination are accepted.
type defaultFilterValidator struct{}

func (defaultFilterValidator) validate(src, dst imageInfo) error {
	if !src.readable {
		return &FilterError{FilterIndex: -1, Source: src, Op: "validate source", Err: ErrNilImage}
	}
	if !dst.writable {
		return &FilterError{FilterIndex: -1, Source: src, Destination: dst, hasDestination: true,
			Op: "validate destination", Err: ErrUnsupportedType}
	}
	return nil
}

// intermediateInfo describes the type of the temporary images that connect
// two adjacent filters (see createTempImage): 16-bit straight-alpha NRGBA64.
func intermediateInfo() imageInfo {
	return imageInfo{
		kind:     kindNRGBA64,
		name:     kindNRGBA64.String(),
		depth:    bitDepth16,
		alpha:    alphaStraight,
		readable: true,
		writable: true,
	}
}

// Check validates the filter chain against the given source and destination
// types without decoding any pixels and without allocating images. It returns
// a *FilterError wrapping one of the package sentinel errors when the chain
// cannot be executed; otherwise it returns nil.
//
// The check covers nil (including typed-nil) images, nil filters inside the
// chain, non-writable destinations (for example *image.YCbCr) and the
// parameter checks implemented by the individual filters.
func (g *GIFT) Check(src image.Image, dst draw.Image) error {
	srcInfo := inspectImage(src)
	if !srcInfo.readable {
		return &FilterError{FilterIndex: -1, Source: srcInfo, Op: "validate source", Err: ErrNilImage}
	}
	dstInfo := inspectDrawImage(dst)
	if !dstInfo.writable {
		op := "validate destination"
		err := ErrUnsupportedType
		if isNilInterface(dst) {
			err = ErrNilImage
		}
		return &FilterError{FilterIndex: -1, Source: srcInfo, Destination: dstInfo, hasDestination: true,
			Op: op, Err: err}
	}

	if len(g.Filters) == 0 {
		return nil
	}

	cur := srcInfo
	last := len(g.Filters) - 1
	for i, f := range g.Filters {
		if isNilInterface(f) {
			return &FilterError{FilterIndex: i, FilterName: "<nil>", Op: "validate filter", Err: ErrNilFilter}
		}

		var stepDst imageInfo
		if i == last {
			stepDst = dstInfo
		} else {
			stepDst = intermediateInfo()
		}

		if v, ok := f.(filterValidator); ok {
			if err := v.validate(cur, stepDst); err != nil {
				return g.decorateError(err, i, f, cur, stepDst)
			}
		}
		cur = stepDst
	}
	return nil
}

// decorateError fills in the chain position and type context on errors
// produced by a filter's own validate implementation, leaving already
// complete errors untouched.
func (g *GIFT) decorateError(err error, i int, f Filter, src, dst imageInfo) error {
	var fe *FilterError
	if errors.As(err, &fe) {
		if fe.FilterIndex < 0 {
			fe.FilterIndex = i
		}
		if fe.FilterName == "" {
			fe.FilterName = reflect.TypeOf(f).String()
		}
		if fe.Op == "" {
			fe.Op = "validate filter"
		}
		return fe
	}
	return &FilterError{
		FilterIndex:    i,
		FilterName:     reflect.TypeOf(f).String(),
		Source:         src,
		Destination:    dst,
		hasDestination: true,
		Op:             "validate filter",
		Err:            err,
	}
}

// DrawChecked is Draw with validation: it runs Check first and returns the
// validation error instead of panicking. When the chain is valid the behavior
// and output are identical to Draw.
func (g *GIFT) DrawChecked(dst draw.Image, src image.Image) error {
	if err := g.Check(src, dst); err != nil {
		return err
	}
	g.Draw(dst, src)
	return nil
}

// DrawAtChecked is DrawAt with validation, mirroring DrawChecked. The pt and
// op parameters are accepted as-is; only the image types and filter chain are
// validated ahead of execution.
func (g *GIFT) DrawAtChecked(dst draw.Image, src image.Image, pt image.Point, op Operator) error {
	if err := g.Check(src, dst); err != nil {
		return err
	}
	g.DrawAt(dst, src, pt, op)
	return nil
}

// mustBeValid panics with the readable validation error when the chain cannot
// run. It is the single guard used by the non-checked Draw/DrawAt entry
// points, so an invalid input never reaches a bare nil-pointer panic.
func (g *GIFT) mustBeValid(dst draw.Image, src image.Image) {
	if err := g.Check(src, dst); err != nil {
		panic(err)
	}
}
