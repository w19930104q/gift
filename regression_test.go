package gift

// Pixel regression suite.
//
// Every filter is applied to a set of fixed input images. The resulting
// image is encoded as PNG and its SHA-256 hash is compared against a hash
// committed in testdata/regression_hashes.json. Any drift in a filter's
// output fails exactly one subtest named "<filter>__<input>", so the
// failing filter and input image are identifiable directly from the test
// output. The actual PNG output is written to the failure artifact
// directory for inspection.
//
// Run the suite:
//
//	go test -count=1 -run TestPixelRegression -v .
//
// Regenerate the committed hashes after an intentional change:
//
//	go test -count=1 -run TestPixelRegression . -update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

var regressionUpdate = flag.Bool("update", false, "regenerate testdata/regression_hashes.json from current filter output")

const regressionGoldenPath = "testdata/regression_hashes.json"

// regressionInputs are the fixed input images. Each entry is a factory so
// every case gets a pristine source image.
var regressionInputs = []struct {
	name string
	load func(t *testing.T) image.Image
}{
	{"srcpng", loadSrcPNG},
	{"synthetic", syntheticTestImage},
}

// regressionCase pairs a filter (with fixed parameters) with the inputs it
// runs on. A nil inputs list means all regression inputs.
type regressionCase struct {
	name   string
	filter Filter
	inputs []string
}

// regressionCases covers every exported filter constructor in the package.
// It is a function (not a package-level variable) because the Resampling
// values (NearestNeighborResampling, etc.) are assigned in this package's
// init(), which runs after package-level variable initialization.
func regressionCases() []regressionCase {
	return []regressionCase{
		// colors.go
		{"invert", Invert(), nil},
		{"colorspace_srgb_to_linear", ColorspaceSRGBToLinear(), nil},
		{"colorspace_linear_to_srgb", ColorspaceLinearToSRGB(), nil},
		{"gamma_0.5", Gamma(0.5), nil},
		{"gamma_1.5", Gamma(1.5), nil},
		{"sigmoid", Sigmoid(0.5, 5), nil},
		{"contrast_increase", Contrast(20), nil},
		{"contrast_decrease", Contrast(-20), nil},
		{"brightness_increase", Brightness(10), nil},
		{"brightness_decrease", Brightness(-10), nil},
		{"grayscale", Grayscale(), nil},
		{"sepia", Sepia(100), nil},
		{"hue_rotate", Hue(45), nil},
		{"saturation_increase", Saturation(30), nil},
		{"saturation_decrease", Saturation(-30), nil},
		{"colorize", Colorize(240, 50, 100), nil},
		{"color_balance", ColorBalance(10, -10, 20), nil},
		{"threshold", Threshold(50), nil},
		{"color_func", ColorFunc(func(r0, g0, b0, a0 float32) (r, g, b, a float32) {
			return b0, g0, r0, a0
		}), nil},

		// convolution.go
		{"convolution_sharpen", Convolution([]float32{
			0, -1, 0,
			-1, 5, -1,
			0, -1, 0,
		}, false, false, false, 0), nil},
		{"convolution_emboss", Convolution([]float32{
			-1, -1, 0,
			-1, 1, 1,
			0, 1, 1,
		}, false, false, false, 0), nil},
		{"gaussian_blur", GaussianBlur(1.5), nil},
		{"unsharp_mask", UnsharpMask(1, 1, 0), nil},
		{"mean", Mean(3, false), nil},
		{"mean_disk", Mean(5, true), nil},
		{"sobel", Sobel(), nil},

		// effects.go
		{"pixelate", Pixelate(5), nil},

		// transform.go
		{"rotate90", Rotate90(), nil},
		{"rotate180", Rotate180(), nil},
		{"rotate270", Rotate270(), nil},
		{"flip_horizontal", FlipHorizontal(), nil},
		{"flip_vertical", FlipVertical(), nil},
		{"transpose", Transpose(), nil},
		{"transverse", Transverse(), nil},
		{"rotate_30_cubic", Rotate(30, color.Transparent, CubicInterpolation), nil},
		{"rotate_45_nearest", Rotate(45, color.White, NearestNeighborInterpolation), nil},
		{"crop", Crop(image.Rect(10, 10, 80, 50)), nil},
		{"crop_to_size", CropToSize(80, 50, CenterAnchor), nil},

		// resize.go
		{"resize_nearest", Resize(90, 60, NearestNeighborResampling), nil},
		{"resize_box", Resize(90, 60, BoxResampling), nil},
		{"resize_linear", Resize(90, 60, LinearResampling), nil},
		{"resize_cubic", Resize(90, 60, CubicResampling), nil},
		{"resize_lanczos", Resize(90, 60, LanczosResampling), nil},
		{"resize_to_fit", ResizeToFit(90, 90, LanczosResampling), nil},
		{"resize_to_fill", ResizeToFill(90, 60, LanczosResampling, CenterAnchor), nil},

		// rank.go
		{"median", Median(3, false), nil},
		{"median_disk", Median(5, true), nil},
		{"minimum", Minimum(3, false), nil},
		{"minimum_disk", Minimum(5, true), nil},
		{"maximum", Maximum(3, false), nil},
		{"maximum_disk", Maximum(5, true), nil},
	}
}

type regressionGoldenEntry struct {
	SHA256 string `json:"sha256"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type regressionGoldenFile struct {
	Cases map[string]regressionGoldenEntry `json:"cases"`
}

// loadSrcPNG loads the committed fixed test image testdata/src.png.
func loadSrcPNG(t *testing.T) image.Image {
	t.Helper()
	f, err := os.Open("testdata/src.png")
	if err != nil {
		t.Fatalf("open testdata/src.png: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode testdata/src.png: %v", err)
	}
	return img
}

// syntheticTestImage builds a deterministic synthetic input: gradients plus
// saturated color blocks with varying alpha, at a size different from
// testdata/src.png so size-dependent filters are exercised on two shapes.
func syntheticTestImage(t *testing.T) image.Image {
	t.Helper()
	const w, h = 97, 61
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := img.PixOffset(x, y)
			img.Pix[o+0] = uint8(x * 255 / (w - 1))
			img.Pix[o+1] = uint8(y * 255 / (h - 1))
			img.Pix[o+2] = uint8((x + y) * 255 / (w + h - 2))
			img.Pix[o+3] = 255
		}
	}
	blocks := []struct {
		r image.Rectangle
		c color.NRGBA
	}{
		{image.Rect(5, 5, 25, 20), color.NRGBA{255, 0, 0, 255}},
		{image.Rect(30, 10, 55, 30), color.NRGBA{0, 255, 0, 200}},
		{image.Rect(60, 25, 90, 50), color.NRGBA{0, 0, 255, 128}},
		{image.Rect(10, 40, 40, 58), color.NRGBA{255, 255, 0, 255}},
	}
	for _, b := range blocks {
		for y := b.r.Min.Y; y < b.r.Max.Y; y++ {
			for x := b.r.Min.X; x < b.r.Max.X; x++ {
				img.SetNRGBA(x, y, b.c)
			}
		}
	}
	return img
}

// applyFilterPNG runs f on src and returns the PNG encoding of the result.
func applyFilterPNG(t *testing.T, f Filter, src image.Image) []byte {
	t.Helper()
	g := New(f)
	dst := image.NewRGBA(g.Bounds(src.Bounds()))
	g.Draw(dst, src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return buf.Bytes()
}

func pngSHA256(pngBytes []byte) string {
	sum := sha256.Sum256(pngBytes)
	return hex.EncodeToString(sum[:])
}

// sortStrings sorts a string slice in place (the package-level sort in
// utils.go shadows the standard library's sort package).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// regressionRuns expands the case table into (case, input) pairs.
func regressionRuns(t *testing.T) []struct {
	caseName  string
	inputName string
	filter    Filter
	load      func(t *testing.T) image.Image
} {
	inputByName := map[string]func(t *testing.T) image.Image{}
	var inputOrder []string
	for _, in := range regressionInputs {
		inputByName[in.name] = in.load
		inputOrder = append(inputOrder, in.name)
	}
	var runs []struct {
		caseName  string
		inputName string
		filter    Filter
		load      func(t *testing.T) image.Image
	}
	for _, c := range regressionCases() {
		names := c.inputs
		if len(names) == 0 {
			names = inputOrder
		}
		for _, inName := range names {
			load, ok := inputByName[inName]
			if !ok {
				t.Fatalf("case %q references unknown input %q", c.name, inName)
			}
			runs = append(runs, struct {
				caseName  string
				inputName string
				filter    Filter
				load      func(t *testing.T) image.Image
			}{c.name, inName, c.filter, load})
		}
	}
	return runs
}

func regressionCaseID(caseName, inputName string) string {
	return caseName + "__" + inputName
}

func loadRegressionGolden(t *testing.T) regressionGoldenFile {
	t.Helper()
	data, err := os.ReadFile(regressionGoldenPath)
	if err != nil {
		t.Fatalf("read %s: %v (run with -update to create it)", regressionGoldenPath, err)
	}
	var golden regressionGoldenFile
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("parse %s: %v", regressionGoldenPath, err)
	}
	return golden
}

func writeRegressionGolden(t *testing.T, golden regressionGoldenFile) {
	t.Helper()
	data, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(regressionGoldenPath, data, 0644); err != nil {
		t.Fatalf("write %s: %v", regressionGoldenPath, err)
	}
}

func TestPixelRegression(t *testing.T) {
	runs := regressionRuns(t)

	if *regressionUpdate {
		golden := regressionGoldenFile{Cases: map[string]regressionGoldenEntry{}}
		for _, r := range runs {
			src := r.load(t)
			out := applyFilterPNG(t, r.filter, src)
			g := New(r.filter)
			b := g.Bounds(src.Bounds())
			golden.Cases[regressionCaseID(r.caseName, r.inputName)] = regressionGoldenEntry{
				SHA256: pngSHA256(out),
				Width:  b.Dx(),
				Height: b.Dy(),
			}
		}
		writeRegressionGolden(t, golden)
		t.Logf("updated %s with %d cases", regressionGoldenPath, len(golden.Cases))
		return
	}

	golden := loadRegressionGolden(t)

	defined := map[string]bool{}
	for _, r := range runs {
		id := regressionCaseID(r.caseName, r.inputName)
		if defined[id] {
			t.Fatalf("duplicate regression case id %q", id)
		}
		defined[id] = true

		t.Run(id, func(t *testing.T) {
			src := r.load(t)

			// Run twice: identical output hashes prove the filter is
			// deterministic under GIFT's parallel draw.
			out1 := applyFilterPNG(t, r.filter, src)
			out2 := applyFilterPNG(t, r.filter, src)
			hash1, hash2 := pngSHA256(out1), pngSHA256(out2)
			if hash1 != hash2 {
				t.Fatalf("nondeterministic output: filter=%s input=%s run1=%s run2=%s",
					r.caseName, r.inputName, hash1, hash2)
			}

			want, ok := golden.Cases[id]
			if !ok {
				t.Fatalf("no golden hash for filter=%s input=%s (case %q); run `go test -run TestPixelRegression . -update` to add it",
					r.caseName, r.inputName, id)
			}
			if hash1 != want.SHA256 {
				artifactDir := filepath.Join(os.TempDir(), "gift-pixel-regression")
				if err := os.MkdirAll(artifactDir, 0755); err == nil {
					artifact := filepath.Join(artifactDir, id+".png")
					if werr := os.WriteFile(artifact, out1, 0644); werr == nil {
						t.Logf("actual output written to %s", artifact)
					}
				}
				t.Fatalf("pixel drift detected: filter=%s input=%s case=%s golden sha256=%s actual sha256=%s",
					r.caseName, r.inputName, id, want.SHA256, hash1)
			}
		})
	}

	// Golden entries with no matching case are stale and hide removed
	// coverage; fail so the golden file stays in sync with the case table.
	var stale []string
	for id := range golden.Cases {
		if !defined[id] {
			stale = append(stale, id)
		}
	}
	if len(stale) > 0 {
		sortStrings(stale)
		t.Fatalf("stale golden entries in %s (run with -update to prune): %v", regressionGoldenPath, stale)
	}
}

// TestPixelRegressionCoverage guards the suite itself: every exported filter
// constructor must appear in the case table so new filters cannot ship
// without regression coverage.
func TestPixelRegressionCoverage(t *testing.T) {
	required := []string{
		"invert", "colorspace_srgb_to_linear", "colorspace_linear_to_srgb",
		"gamma", "sigmoid", "contrast", "brightness", "grayscale", "sepia",
		"hue", "saturation", "colorize", "color_balance", "threshold", "color_func",
		"convolution", "gaussian_blur", "unsharp_mask", "mean", "sobel",
		"pixelate",
		"rotate90", "rotate180", "rotate270", "flip_horizontal", "flip_vertical",
		"transpose", "transverse", "rotate", "crop", "crop_to_size",
		"resize", "resize_to_fit", "resize_to_fill",
		"median", "minimum", "maximum",
	}
	seen := map[string]bool{}
	for _, c := range regressionCases() {
		seen[c.name] = true
	}
	var missing []string
	for _, prefix := range required {
		found := false
		for name := range seen {
			if name == prefix || len(name) > len(prefix) && name[:len(prefix)+1] == prefix+"_" {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, prefix)
		}
	}
	if len(missing) > 0 {
		sortStrings(missing)
		t.Fatalf("filters missing from regression case table: %v", missing)
	}
}
