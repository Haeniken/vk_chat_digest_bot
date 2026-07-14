package app

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"bot-summary-vk/internal/storage"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func renderLLMUsageChart(daily []storage.DailyLLMUsage) ([]byte, error) {
	if len(daily) == 0 {
		return nil, fmt.Errorf("no daily usage data")
	}

	points := make([]storage.DailyLLMUsage, len(daily))
	for i := range daily {
		points[len(daily)-1-i] = daily[i]
	}

	const (
		width       = 1200
		height      = 720
		leftMargin  = 96
		rightMargin = 56
		topMargin   = 96
		botMargin   = 104
	)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := color.RGBA{R: 9, G: 19, B: 28, A: 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	drawVignette(img)

	ink := color.RGBA{R: 229, G: 235, B: 229, A: 255}
	muted := color.RGBA{R: 139, G: 157, B: 165, A: 255}
	grid := color.RGBA{R: 44, G: 67, B: 78, A: 255}
	inputColor := color.RGBA{R: 36, G: 197, B: 204, A: 255}
	outputColor := color.RGBA{R: 221, G: 62, B: 58, A: 255}
	warm := color.RGBA{R: 235, G: 178, B: 87, A: 255}

	drawText(img, 64, 48, "LLM tokens: input / output", ink)
	drawText(img, 64, 72, "last 7 days", muted)
	drawLegend(img, width-360, 48, inputColor, "input", outputColor, "output")

	plotW := width - leftMargin - rightMargin
	plotH := height - topMargin - botMargin
	baseY := topMargin + plotH

	maxTokens := int64(0)
	for _, p := range points {
		if p.PromptTokens > maxTokens {
			maxTokens = p.PromptTokens
		}
		if p.CompletionTokens > maxTokens {
			maxTokens = p.CompletionTokens
		}
	}
	if maxTokens <= 0 {
		maxTokens = 1
	}

	for i := 0; i <= 4; i++ {
		y := topMargin + plotH - (plotH*i)/4
		drawHLine(img, leftMargin, width-rightMargin, y, grid)
		labelValue := maxTokens * int64(i) / 4
		drawText(img, 28, y+5, formatTokenCount(labelValue), muted)
	}
	drawHLine(img, leftMargin, width-rightMargin, baseY, color.RGBA{R: 89, G: 109, B: 116, A: 255})
	drawVLine(img, leftMargin, topMargin, baseY, color.RGBA{R: 89, G: 109, B: 116, A: 255})

	band := plotW / len(points)
	barW := clampInt(band/5, 22, 54)
	for i, p := range points {
		center := leftMargin + band*i + band/2
		inputH := int(float64(p.PromptTokens) / float64(maxTokens) * float64(plotH-12))
		outputH := int(float64(p.CompletionTokens) / float64(maxTokens) * float64(plotH-12))
		if p.PromptTokens > 0 && inputH < 2 {
			inputH = 2
		}
		if p.CompletionTokens > 0 && outputH < 2 {
			outputH = 2
		}

		drawBar(img, center-barW-4, baseY-inputH, barW, inputH, inputColor)
		drawBar(img, center+4, baseY-outputH, barW, outputH, outputColor)

		dateLabel := compactDateLabel(p.Day)
		drawText(img, center-24, baseY+28, dateLabel, ink)
		drawText(img, center-barW-12, baseY-inputH-10, formatTokenCount(p.PromptTokens), inputColor)
		drawText(img, center+2, baseY-outputH-10, formatTokenCount(p.CompletionTokens), outputColor)
	}

	drawText(img, 64, height-36, "Daily Drama Digest telemetry", warm)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode chart png: %w", err)
	}
	return buf.Bytes(), nil
}

func drawText(img *image.RGBA, x, y int, text string, c color.Color) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
}

func drawLegend(img *image.RGBA, x, y int, inputColor color.RGBA, inputLabel string, outputColor color.RGBA, outputLabel string) {
	drawBar(img, x, y-12, 34, 12, inputColor)
	drawText(img, x+44, y, inputLabel, color.RGBA{R: 229, G: 235, B: 229, A: 255})
	drawBar(img, x+132, y-12, 34, 12, outputColor)
	drawText(img, x+176, y, outputLabel, color.RGBA{R: 229, G: 235, B: 229, A: 255})
}

func drawBar(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	draw.Draw(img, image.Rect(x, y, x+w, y+h), &image.Uniform{C: c}, image.Point{}, draw.Src)
	highlight := color.RGBA{R: minByte(int(c.R) + 42), G: minByte(int(c.G) + 42), B: minByte(int(c.B) + 42), A: 190}
	draw.Draw(img, image.Rect(x, y, x+w, y+3), &image.Uniform{C: highlight}, image.Point{}, draw.Src)
}

func drawHLine(img *image.RGBA, x1, x2, y int, c color.RGBA) {
	draw.Draw(img, image.Rect(x1, y, x2, y+1), &image.Uniform{C: c}, image.Point{}, draw.Src)
}

func drawVLine(img *image.RGBA, x, y1, y2 int, c color.RGBA) {
	draw.Draw(img, image.Rect(x, y1, x+1, y2), &image.Uniform{C: c}, image.Point{}, draw.Src)
}

func drawVignette(img *image.RGBA) {
	bounds := img.Bounds()
	cx := float64(bounds.Dx()) / 2
	cy := float64(bounds.Dy()) / 2
	maxDistance := cx*cx + cy*cy
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			factor := (dx*dx + dy*dy) / maxDistance
			if factor < 0.35 {
				continue
			}
			shade := uint8((factor - 0.35) * 95)
			p := img.RGBAAt(x, y)
			p.R = subByte(p.R, shade)
			p.G = subByte(p.G, shade)
			p.B = subByte(p.B, shade)
			img.SetRGBA(x, y, p)
		}
	}
}

func compactDateLabel(day string) string {
	day = strings.TrimSpace(day)
	if len(day) >= len("2006-01-02") {
		return day[5:]
	}
	if day == "" {
		return "-"
	}
	return day
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func minByte(value int) uint8 {
	if value > 255 {
		return 255
	}
	if value < 0 {
		return 0
	}
	return uint8(value)
}

func subByte(value, sub uint8) uint8 {
	if sub > value {
		return 0
	}
	return value - sub
}
