package imageutil

import (
	"fmt"
	"image"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
)

// ProcessOptions mengatur parameter pemrosesan satu gambar.
type ProcessOptions struct {
	MaxWidth      int    // resize proporsional jika lebar > MaxWidth (0 = skip resize)
	WatermarkPath string // path ke file watermark PNG (opsional, transparan disarankan)
	JPEGQuality   int    // 1-100, default 85 jika 0
}

// Result berisi path akhir file hasil proses.
type Result struct {
	OutputPath string
}

// Process melakukan resize -> watermark (opsional) -> simpan sebagai JPEG.
//
// Catatan: versi ini output JPEG, bukan WebP. Convert ke WebP butuh binary
// eksternal (cwebp) atau library cgo -- di-skip dulu di v1 biar setup lebih
// simpel, dicatat sebagai "next improvement" di README.
func Process(srcPath, destDir string, opts ProcessOptions) (*Result, error) {
	if opts.JPEGQuality == 0 {
		opts.JPEGQuality = 85
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("gagal membuat destDir: %w", err)
	}

	img, err := imaging.Open(srcPath, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("gagal membuka gambar sumber: %w", err)
	}

	if opts.MaxWidth > 0 && img.Bounds().Dx() > opts.MaxWidth {
		img = imaging.Resize(img, opts.MaxWidth, 0, imaging.Lanczos)
	}

	if opts.WatermarkPath != "" {
		img, err = applyWatermark(img, opts.WatermarkPath)
		if err != nil {
			return nil, fmt.Errorf("gagal menerapkan watermark: %w", err)
		}
	}

	base := trimExt(filepath.Base(srcPath))
	outputPath := filepath.Join(destDir, base+".jpg")

	if err := imaging.Save(img, outputPath, imaging.JPEGQuality(opts.JPEGQuality)); err != nil {
		return nil, fmt.Errorf("gagal menyimpan hasil: %w", err)
	}

	return &Result{OutputPath: outputPath}, nil
}

// applyWatermark menempelkan watermark PNG di pojok kanan bawah gambar.
func applyWatermark(img image.Image, watermarkPath string) (image.Image, error) {
	wm, err := imaging.Open(watermarkPath, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("gagal membuka file watermark: %w", err)
	}

	// Skala watermark maks 20% dari lebar gambar utama biar proporsional.
	maxWMWidth := img.Bounds().Dx() / 5
	if wm.Bounds().Dx() > maxWMWidth {
		wm = imaging.Resize(wm, maxWMWidth, 0, imaging.Lanczos)
	}

	const padding = 16
	offset := image.Pt(
		img.Bounds().Dx()-wm.Bounds().Dx()-padding,
		img.Bounds().Dy()-wm.Bounds().Dy()-padding,
	)

	result := imaging.Overlay(img, wm, offset, 0.8)
	return result, nil
}

func trimExt(filename string) string {
	ext := filepath.Ext(filename)
	return filename[:len(filename)-len(ext)]
}