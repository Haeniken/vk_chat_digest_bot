package imagegen

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func withDDDWatermark(imageBytes []byte) []byte {
	watermarked, err := addDDDWatermark(imageBytes)
	if err != nil {
		return imageBytes
	}
	return watermarked
}

func addDDDWatermark(imageBytes []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	canvas := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(canvas, canvas.Bounds(), img, bounds.Min, draw.Src)

	drawDDDWatermark(canvas)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, canvas, &jpeg.Options{Quality: 92}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func drawDDDWatermark(dst *image.RGBA) {
	bounds := dst.Bounds()
	minSide := math.Min(float64(bounds.Dx()), float64(bounds.Dy()))
	fontSize := clampFloat(minSide*0.092, 42, 112)
	margin := int(clampFloat(minSide*0.04, 24, 58))

	ttf, err := opentype.Parse(gobold.TTF)
	if err != nil {
		return
	}
	face, err := opentype.NewFace(ttf, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return
	}
	defer face.Close()

	measure := font.Drawer{Face: face}
	text := "DDD"
	textWidth := measure.MeasureString(text).Ceil()
	underlineThickness := int(clampFloat(minSide*0.004, 2, 5))
	underlineGap := int(clampFloat(fontSize*0.08, 5, 12))
	shadowOffset := int(clampFloat(minSide*0.004, 2, 5))

	x := bounds.Max.X - margin - textWidth
	baseline := bounds.Max.Y - margin - underlineThickness - underlineGap
	if x < margin {
		x = margin
	}

	drawWatermarkText(dst, face, text, x+shadowOffset, baseline+shadowOffset, color.RGBA{0, 0, 0, 70})
	drawWatermarkText(dst, face, "D", x, baseline, color.RGBA{248, 244, 232, 155})
	secondX := x + measure.MeasureString("D").Ceil()
	drawWatermarkText(dst, face, "D", secondX, baseline, color.RGBA{215, 0, 0, 170})
	thirdX := secondX + measure.MeasureString("D").Ceil()
	drawWatermarkText(dst, face, "D", thirdX, baseline, color.RGBA{248, 244, 232, 240})

	lineY := baseline + underlineGap
	lineColor := color.RGBA{190, 0, 0, 145}
	drawFilledRect(dst, image.Rect(x, lineY, x+textWidth, lineY+underlineThickness), lineColor)
	drawDiamond(dst, x+textWidth/2, lineY+underlineThickness/2, underlineThickness*2, lineColor)
}

func drawWatermarkText(dst *image.RGBA, face font.Face, text string, x, baseline int, src color.Color) {
	drawer := font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(src),
		Face: face,
		Dot:  fixed.P(x, baseline),
	}
	drawer.DrawString(text)
}

func drawFilledRect(dst *image.RGBA, rect image.Rectangle, src color.Color) {
	draw.Draw(dst, rect, image.NewUniform(src), image.Point{}, draw.Over)
}

func drawDiamond(dst *image.RGBA, cx, cy, radius int, src color.Color) {
	if radius < 2 {
		radius = 2
	}
	rgba := color.RGBAModel.Convert(src).(color.RGBA)
	for y := cy - radius; y <= cy+radius; y++ {
		for x := cx - radius; x <= cx+radius; x++ {
			if absInt(x-cx)+absInt(y-cy) <= radius {
				dst.SetRGBA(x, y, rgba)
			}
		}
	}
}

func clampFloat(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func init() {
	image.RegisterFormat("jpeg", "\xff\xd8", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("png", "\x89PNG\r\n\x1a\n", png.Decode, png.DecodeConfig)
}
