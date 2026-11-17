package gift

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"
)

// TestInspectImage exercises the type descriptor for every image kind the
// pipeline knows about, including the alpha and bit-depth semantics.
func TestInspectImage(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	cases := []struct {
		img      image.Image
		kind     imageKind
		depth    bitDepth
		alpha    alphaMode
		writable bool
		readable bool
		hasAlpha bool
		is16Bit  bool
		typeName string
	}{
		{image.NewNRGBA(r), kindNRGBA, bitDepth8, alphaStraight, true, true, true, false, "*image.NRGBA"},
		{image.NewNRGBA64(r), kindNRGBA64, bitDepth16, alphaStraight, true, true, true, true, "*image.NRGBA64"},
		{image.NewRGBA(r), kindRGBA, bitDepth8, alphaPremultiplied, true, true, true, false, "*image.RGBA"},
		{image.NewRGBA64(r), kindRGBA64, bitDepth16, alphaPremultiplied, true, true, true, true, "*image.RGBA64"},
		{image.NewGray(r), kindGray, bitDepth8, alphaNone, true, true, false, false, "*image.Gray"},
		{image.NewGray16(r), kindGray16, bitDepth16, alphaNone, true, true, false, true, "*image.Gray16"},
		{image.NewYCbCr(r, image.YCbCrSubsampleRatio444), kindYCbCr, bitDepth8, alphaNone, false, true, false, false, "*image.YCbCr"},
		{image.NewPaletted(r, color.Palette{color.Black, color.White}), kindPaletted, bitDepth8, alphaPalette, true, true, true, false, "*image.Paletted"},
		{image.NewAlpha(r), kindGeneric, bitDepth16, alphaGeneric, true, true, true, true, "*image.Alpha"},
		{image.NewUniform(color.NRGBA{}), kindGeneric, bitDepth16, alphaGeneric, false, true, true, true, "*image.Uniform"},
	}
	for _, c := range cases {
		info := inspectImage(c.img)
		if info.kind != c.kind || info.depth != c.depth || info.alpha != c.alpha ||
			info.writable != c.writable || info.readable != c.readable ||
			info.hasAlpha() != c.hasAlpha || info.is16Bit() != c.is16Bit ||
			info.name != c.typeName {
			t.Errorf("inspectImage(%s) = %+v, want kind=%v depth=%d alpha=%v writable=%v readable=%v hasAlpha=%v is16Bit=%v name=%q",
				c.typeName, info, c.kind, c.depth, c.alpha, c.writable, c.readable, c.hasAlpha, c.is16Bit, c.typeName)
		}
	}
}

func TestInspectImageNil(t *testing.T) {
	// Plain nil interface.
	info := inspectImage(nil)
	if info.readable || info.kind != kindUnknown {
		t.Errorf("inspectImage(nil) = %+v, want unreadable unknown kind", info)
	}

	// Typed nil pointer: a plain == nil check would miss it.
	var nrgba *image.NRGBA
	info = inspectImage(nrgba)
	if info.readable || info.kind != kindUnknown {
		t.Errorf("inspectImage(typed-nil NRGBA) = %+v, want unreadable unknown kind", info)
	}

	// Typed nil draw.Image destination.
	var rgba64 *image.RGBA64
	info = inspectDrawImage(rgba64)
	if info.writable || info.readable {
		t.Errorf("inspectDrawImage(typed-nil RGBA64) = %+v, want non-writable", info)
	}
}

// TestCheckNilInputs is the error-injection matrix for the entry-point
// validations. Every failure must surface a readable *FilterError wrapping a
// documented sentinel.
func TestCheckNilInputs(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	src := image.NewNRGBA(r)
	dst := image.NewNRGBA(r)

	// YCbCr is readable but not writable (see TestInspectImage). A non-nil
	// value can only reach the draw.Image slot when it implements Set, so the
	// non-writable branch of the validator is exercised directly below.
	var nonWritable image.Image = image.NewYCbCr(r, image.YCbCrSubsampleRatio444)

	t.Run("nil source", func(t *testing.T) {
		g := New(Invert())
		err := g.Check(nil, dst)
		assertFilterError(t, err, ErrNilImage, -1)
		if !strings.Contains(err.Error(), "source image is nil") {
			t.Errorf("error not readable enough: %q", err.Error())
		}
	})

	t.Run("typed-nil source", func(t *testing.T) {
		var gray *image.Gray
		g := New(Invert())
		err := g.Check(gray, dst)
		assertFilterError(t, err, ErrNilImage, -1)
	})

	t.Run("nil destination", func(t *testing.T) {
		g := New(Invert())
		err := g.Check(src, nil)
		assertFilterError(t, err, ErrNilImage, -1)
		if !strings.Contains(err.Error(), "destination image is nil") {
			t.Errorf("error not readable enough: %q", err.Error())
		}
	})

	t.Run("typed-nil destination", func(t *testing.T) {
		var nrgba64 *image.NRGBA64
		g := New(Invert())
		err := g.Check(src, nrgba64)
		assertFilterError(t, err, ErrNilImage, -1)
	})

	t.Run("non-writable destination", func(t *testing.T) {
		g := New(Invert())
		if err := g.Check(nonWritable, dst); err != nil {
			t.Errorf("read-only image as source should be accepted, got: %v", err)
		}
		// The only concrete non-writable type is represented by YCbCr's
		// descriptor; drive the validator with it directly.
		readOnly := inspectImage(nonWritable)
		if readOnly.writable {
			t.Fatalf("test setup: expected non-writable descriptor, got %+v", readOnly)
		}
		err := defaultFilterValidator{}.validate(srcInfo(r), readOnly)
		assertFilterError(t, err, ErrUnsupportedType, -1)
		if !strings.Contains(err.Error(), "unsupported image type") {
			t.Errorf("error must explain the type problem: %q", err.Error())
		}
	})

	t.Run("nil filter in chain", func(t *testing.T) {
		g := New(Invert(), nil, Grayscale())
		err := g.Check(src, dst)
		assertFilterError(t, err, ErrNilFilter, 1)
		if !strings.Contains(err.Error(), "chain index 1") {
			t.Errorf("error must name the chain position: %q", err.Error())
		}
	})

	t.Run("typed-nil filter in chain", func(t *testing.T) {
		var broken *colorchanFilter
		g := New(Invert(), broken)
		err := g.Check(src, dst)
		assertFilterError(t, err, ErrNilFilter, 1)
	})

	t.Run("nil resampling", func(t *testing.T) {
		g := New(Resize(1, 1, nil))
		err := g.Check(src, dst)
		assertFilterError(t, err, ErrInvalidFilter, 0)
		if !strings.Contains(err.Error(), "resampling") {
			t.Errorf("error must mention resampling: %q", err.Error())
		}
	})

	t.Run("nil resampling in multi-filter chain", func(t *testing.T) {
		g := New(Invert(), ResizeToFit(1, 1, nil), Grayscale())
		err := g.Check(src, dst)
		assertFilterError(t, err, ErrInvalidFilter, 1)
	})

	t.Run("nil background color", func(t *testing.T) {
		g := New(Rotate(30, nil, LinearInterpolation))
		err := g.Check(src, image.NewNRGBA(g.Bounds(src.Bounds())))
		assertFilterError(t, err, ErrInvalidFilter, 0)
		if !strings.Contains(err.Error(), "background color") {
			t.Errorf("error must mention background color: %q", err.Error())
		}
	})

	t.Run("valid chain", func(t *testing.T) {
		g := New(Invert(), Grayscale(), Resize(1, 1, NearestNeighborResampling))
		if err := g.Check(src, image.NewNRGBA(g.Bounds(r))); err != nil {
			t.Errorf("valid chain rejected: %v", err)
		}
	})

	t.Run("empty chain", func(t *testing.T) {
		g := New()
		if err := g.Check(src, dst); err != nil {
			t.Errorf("empty chain rejected: %v", err)
		}
	})
}

// rejectingFilter is a third-party style filter whose validator rejects Gray
// sources, used to verify custom validators participate in Check.
type rejectingFilter struct{}

func (rejectingFilter) Bounds(b image.Rectangle) image.Rectangle { return b }
func (rejectingFilter) Draw(dst draw.Image, src image.Image, options *Options) {
	copyimage(dst, src, options)
}
func (rejectingFilter) validate(src, dst imageInfo) error {
	if src.kind == kindGray {
		return &FilterError{FilterIndex: -1, Source: src, Destination: dst, hasDestination: true,
			Op: "validate source", Err: ErrUnsupportedType}
	}
	return nil
}

// plainErrorFilter returns a non-FilterError from its validator, exercising
// GIFT.decorateError's generic wrapping branch.
type plainErrorFilter struct{}

func (plainErrorFilter) Bounds(b image.Rectangle) image.Rectangle { return b }
func (plainErrorFilter) Draw(dst draw.Image, src image.Image, options *Options) {
	copyimage(dst, src, options)
}
func (plainErrorFilter) validate(src, dst imageInfo) error {
	return errors.New("custom filter exploded")
}

func TestCheckWrapsPlainValidatorError(t *testing.T) {
	g := New(plainErrorFilter{})
	err := g.Check(image.NewNRGBA(image.Rect(0, 0, 1, 1)), image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	if err == nil {
		t.Fatal("expected error")
	}
	var fe *FilterError
	if !errors.As(err, &fe) {
		t.Fatalf("plain validator error must be wrapped in *FilterError, got %T", err)
	}
	if fe.FilterIndex != 0 || !strings.Contains(err.Error(), "custom filter exploded") {
		t.Fatalf("unexpected wrapped error: %v", err)
	}
}

func TestCheckCustomFilterValidator(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	// The rejecting filter is placed first: later filters only ever see the
	// chain's internal NRGBA64 intermediate type, never the original source.
	g := New(rejectingFilter{}, Invert())

	// Accepted for a color source.
	if err := g.Check(image.NewNRGBA(r), image.NewNRGBA(r)); err != nil {
		t.Errorf("custom validator rejected supported type: %v", err)
	}

	// Rejected for a Gray source, with the chain index filled in.
	err := g.Check(image.NewGray(r), image.NewNRGBA(r))
	assertFilterError(t, err, ErrUnsupportedType, 0)
	if !strings.Contains(err.Error(), "*image.Gray") {
		t.Errorf("error must name the offending type: %q", err.Error())
	}
}

func TestDrawCheckedReturnsErrorAndDoesNotDraw(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	dst := image.NewNRGBA(r)

	t.Run("nil source", func(t *testing.T) {
		g := New(Invert())
		if err := g.DrawChecked(dst, nil); err == nil {
			t.Fatal("expected error for nil source")
		} else {
			assertFilterError(t, err, ErrNilImage, -1)
		}
	})

	t.Run("nil filter", func(t *testing.T) {
		src := image.NewNRGBA(r)
		g := New(nil)
		if err := g.DrawChecked(dst, src); err == nil {
			t.Fatal("expected error for nil filter")
		} else {
			assertFilterError(t, err, ErrNilFilter, 0)
		}
	})

	t.Run("DrawAt checked", func(t *testing.T) {
		g := New(Invert())
		if err := g.DrawAtChecked(dst, nil, image.Pt(0, 0), CopyOperator); err == nil {
			t.Fatal("expected error for nil source")
		}
	})
}

// TestDrawPanicsWithReadableError ensures the legacy Draw/DrawAt entry points
// no longer fail with bare nil-pointer panics: they panic with the same
// *FilterError the checked API returns.
func TestDrawPanicsWithReadableError(t *testing.T) {
	r := image.Rect(0, 0, 2, 2)
	dst := image.NewNRGBA(r)

	assertPanic := func(t *testing.T, fn func()) {
		t.Helper()
		defer func() {
			rec := recover()
			if rec == nil {
				t.Fatal("expected panic, got none")
			}
			fe, ok := rec.(*FilterError)
			if !ok {
				t.Fatalf("expected *FilterError panic, got %T: %v", rec, rec)
			}
			if fe.Error() == "" || !strings.Contains(fe.Error(), "gift") {
				t.Fatalf("panic error not readable: %v", fe)
			}
		}()
		fn()
	}

	t.Run("Draw nil source", func(t *testing.T) {
		assertPanic(t, func() { New(Invert()).Draw(dst, nil) })
	})
	t.Run("Draw nil destination", func(t *testing.T) {
		assertPanic(t, func() { New(Invert()).Draw(nil, image.NewNRGBA(r)) })
	})
	t.Run("Draw nil filter", func(t *testing.T) {
		assertPanic(t, func() { New(nil).Draw(dst, image.NewNRGBA(r)) })
	})
	t.Run("Draw nil resampling", func(t *testing.T) {
		assertPanic(t, func() { New(Resize(1, 1, nil)).Draw(dst, image.NewNRGBA(r)) })
	})
	t.Run("DrawAt nil source", func(t *testing.T) {
		assertPanic(t, func() { New(Invert()).DrawAt(dst, nil, image.Pt(0, 0), CopyOperator) })
	})
}

func TestErrorSentinelsAndWrapping(t *testing.T) {
	g := New(nil)
	err := g.Check(image.NewNRGBA(image.Rect(0, 0, 1, 1)), image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	var fe *FilterError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FilterError, got %T", err)
	}
	if !errors.Is(err, ErrNilFilter) {
		t.Fatalf("errors.Is must match ErrNilFilter, got %v", err)
	}
	if fe.FilterIndex != 0 {
		t.Fatalf("expected index 0, got %d", fe.FilterIndex)
	}
}

func TestIntermediateInfoIsNRGBA64(t *testing.T) {
	info := intermediateInfo()
	if info.kind != kindNRGBA64 || !info.is16Bit() || info.alpha != alphaStraight ||
		!info.readable || !info.writable {
		t.Fatalf("intermediate images must be 16-bit straight-alpha NRGBA64, got %+v", info)
	}
}

func TestImageKindString(t *testing.T) {
	want := map[imageKind]string{
		kindNRGBA:    "*image.NRGBA",
		kindNRGBA64:  "*image.NRGBA64",
		kindRGBA:     "*image.RGBA",
		kindRGBA64:   "*image.RGBA64",
		kindGray:     "*image.Gray",
		kindGray16:   "*image.Gray16",
		kindYCbCr:    "*image.YCbCr",
		kindPaletted: "*image.Paletted",
		kindGeneric:  "generic image.Image",
		kindUnknown:  "unknown image type",
	}
	for k, name := range want {
		if got := k.String(); got != name {
			t.Errorf("imageKind(%d).String() = %q, want %q", k, got, name)
		}
	}
}

func TestFilterErrorMessageBranches(t *testing.T) {
	r := image.Rect(0, 0, 1, 1)
	info := inspectImage(image.NewGray(r))
	cases := []struct {
		name string
		err  *FilterError
		want []string
	}{
		{"nil source", &FilterError{Source: info, Op: "validate source", Err: ErrNilImage},
			[]string{"gift", "source image is nil"}},
		{"nil destination", &FilterError{Source: info, Op: "validate destination", Err: ErrNilImage},
			[]string{"destination image is nil"}},
		{"unsupported with destination", &FilterError{FilterIndex: 2, FilterName: "x", Source: info,
			Destination: info, hasDestination: true, Op: "validate destination", Err: ErrUnsupportedType},
			[]string{"filter #2", "source=", "destination="}},
		{"unsupported source only", &FilterError{Source: info, Op: "validate source", Err: ErrUnsupportedType},
			[]string{"image="}},
		{"custom wrapped error", &FilterError{FilterIndex: 0, FilterName: "x", Op: "validate filter",
			Err: errors.New("boom")}, []string{"boom", "validate filter"}},
	}
	for _, c := range cases {
		msg := c.err.Error()
		for _, frag := range c.want {
			if !strings.Contains(msg, frag) {
				t.Errorf("%s: message %q missing %q", c.name, msg, frag)
			}
		}
	}
}

// srcInfo returns the descriptor of a fresh NRGBA image with the given bounds.
func srcInfo(r image.Rectangle) imageInfo {
	return inspectImage(image.NewNRGBA(r))
}

func assertFilterError(t *testing.T, err error, wantSentinel error, wantIndex int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error wrapping %v, got nil", wantSentinel)
	}
	if !errors.Is(err, wantSentinel) {
		t.Fatalf("expected errors.Is(err, %v) to be true, got %v", wantSentinel, err)
	}
	var fe *FilterError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FilterError, got %T: %v", err, err)
	}
	if wantIndex >= 0 && fe.FilterIndex != wantIndex {
		t.Fatalf("expected FilterIndex %d, got %d (error: %v)", wantIndex, fe.FilterIndex, err)
	}
	if !strings.Contains(err.Error(), "gift") {
		t.Fatalf("error message should identify the package: %q", err.Error())
	}
}
