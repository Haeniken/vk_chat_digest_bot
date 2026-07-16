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

func renderUsageChart(daily []storage.DailyLLMUsage, imageDaily []storage.DailyImageUsage) ([]byte, error) {
	if len(daily) == 0 {
		return nil, fmt.Errorf("no daily usage data")
	}

	llmInputColor := color.RGBA{R: 36, G: 197, B: 204, A: 255}
	llmOutputColor := color.RGBA{R: 221, G: 62, B: 58, A: 255}
	promptInputColor := color.RGBA{R: 77, G: 216, B: 141, A: 255}
	promptOutputColor := color.RGBA{R: 235, G: 178, B: 87, A: 255}
	imageInputColor := color.RGBA{R: 92, G: 144, B: 239, A: 255}
	imageOutputColor := color.RGBA{R: 235, G: 118, B: 72, A: 255}
	points := usageStackedChartPoints(daily, imageDaily, chartPalette{
		LLMInput:     llmInputColor,
		LLMOutput:    llmOutputColor,
		PromptInput:  promptInputColor,
		PromptOutput: promptOutputColor,
		ImageInput:   imageInputColor,
		ImageOutput:  imageOutputColor,
	})
	inputLegend := []usageChartSegment{
		{Label: "summary in", Color: llmInputColor},
		{Label: "prompt in", Color: promptInputColor},
		{Label: "image in", Color: imageInputColor},
	}
	outputLegend := []usageChartSegment{
		{Label: "summary out", Color: llmOutputColor},
		{Label: "prompt out", Color: promptOutputColor},
		{Label: "image out", Color: imageOutputColor},
	}

	const (
		width       = 1200
		height      = 820
		leftMargin  = 96
		rightMargin = 56
	)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := color.RGBA{R: 9, G: 19, B: 28, A: 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	drawVignette(img)

	ink := color.RGBA{R: 229, G: 235, B: 229, A: 255}
	muted := color.RGBA{R: 139, G: 157, B: 165, A: 255}
	grid := color.RGBA{R: 44, G: 67, B: 78, A: 255}
	warm := color.RGBA{R: 235, G: 178, B: 87, A: 255}

	drawText(img, 64, 48, "Token spend by day", ink)

	drawStackedChartPanel(img, stackedChartPanelConfig{
		Title:  "All token sources",
		Points: points,
		Left:   leftMargin,
		Right:  width - rightMargin,
		Top:    128,
		Height: 500,
		Ink:    ink,
		Muted:  muted,
		Grid:   grid,
		Axis:   color.RGBA{R: 89, G: 109, B: 116, A: 255},
	})

	drawStackedLegend(img, 64, height-92, inputLegend, outputLegend, muted)
	drawText(img, 64, height-36, "Daily Drama Digest telemetry", warm)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode chart png: %w", err)
	}
	return buf.Bytes(), nil
}

type usageChartPoint struct {
	Day          string
	InputSeries  []usageChartSegment
	OutputSeries []usageChartSegment
}

type usageChartSegment struct {
	Label string
	Value int64
	Color color.RGBA
}

type stackedChartPanelConfig struct {
	Title  string
	Points []usageChartPoint
	Left   int
	Right  int
	Top    int
	Height int
	Ink    color.RGBA
	Muted  color.RGBA
	Grid   color.RGBA
	Axis   color.RGBA
}

func drawStackedChartPanel(img *image.RGBA, cfg stackedChartPanelConfig) {
	if len(cfg.Points) == 0 {
		return
	}

	maxTokens := int64(0)
	for _, p := range cfg.Points {
		if total := segmentTotal(p.InputSeries); total > maxTokens {
			maxTokens = total
		}
		if total := segmentTotal(p.OutputSeries); total > maxTokens {
			maxTokens = total
		}
	}
	if maxTokens <= 0 {
		maxTokens = 1
	}

	plotW := cfg.Right - cfg.Left
	plotH := cfg.Height
	baseY := cfg.Top + plotH

	drawText(img, cfg.Left, cfg.Top-26, cfg.Title, cfg.Ink)
	for i := 0; i <= 4; i++ {
		y := cfg.Top + plotH - (plotH*i)/4
		drawHLine(img, cfg.Left, cfg.Right, y, cfg.Grid)
		labelValue := maxTokens * int64(i) / 4
		drawText(img, 28, y+5, formatTokenCount(labelValue), cfg.Muted)
	}
	drawHLine(img, cfg.Left, cfg.Right, baseY, cfg.Axis)
	drawVLine(img, cfg.Left, cfg.Top, baseY, cfg.Axis)

	band := plotW / len(cfg.Points)
	barW := clampInt(band/5, 22, 54)
	gap := 8
	for i, p := range cfg.Points {
		center := cfg.Left + band*i + band/2
		inputX := center - barW - gap/2
		outputX := center + gap/2
		inputTotal := drawStackedBar(img, inputX, baseY, barW, plotH-12, maxTokens, p.InputSeries)
		outputTotal := drawStackedBar(img, outputX, baseY, barW, plotH-12, maxTokens, p.OutputSeries)
		drawText(img, inputX-4, baseY-barHeight(inputTotal, maxTokens, plotH-12)-10, formatTokenCount(inputTotal), cfg.Ink)
		drawText(img, outputX-4, baseY-barHeight(outputTotal, maxTokens, plotH-12)-10, formatTokenCount(outputTotal), cfg.Ink)
		drawText(img, inputX+2, baseY+16, "in", cfg.Muted)
		drawText(img, outputX, baseY+16, "out", cfg.Muted)

		dateLabel := compactDateLabel(p.Day)
		drawText(img, center-24, baseY+40, dateLabel, cfg.Ink)
	}
}

type chartPalette struct {
	LLMInput     color.RGBA
	LLMOutput    color.RGBA
	PromptInput  color.RGBA
	PromptOutput color.RGBA
	ImageInput   color.RGBA
	ImageOutput  color.RGBA
}

func usageStackedChartPoints(daily []storage.DailyLLMUsage, imageDaily []storage.DailyImageUsage, palette chartPalette) []usageChartPoint {
	imageByDay := make(map[string]storage.DailyImageUsage, len(imageDaily))
	for _, day := range imageDaily {
		imageByDay[day.Day] = day
	}

	points := make([]usageChartPoint, len(daily))
	for i := range daily {
		day := daily[i]
		imageDay := imageByDay[day.Day]
		points[len(daily)-1-i] = usageChartPoint{
			Day: day.Day,
			InputSeries: []usageChartSegment{
				{Label: "summary in", Value: day.PromptTokens, Color: palette.LLMInput},
				{Label: "prompt in", Value: imageDay.PromptLLMPromptTokens, Color: palette.PromptInput},
				{Label: "image in", Value: imageDay.ImageInputTokens, Color: palette.ImageInput},
			},
			OutputSeries: []usageChartSegment{
				{Label: "summary out", Value: day.CompletionTokens, Color: palette.LLMOutput},
				{Label: "prompt out", Value: imageDay.PromptLLMCompletionTokens, Color: palette.PromptOutput},
				{Label: "image out", Value: imageDay.ImageOutputTokens, Color: palette.ImageOutput},
			},
		}
	}
	return points
}

func drawStackedBar(img *image.RGBA, x, baseY, width, maxHeight int, maxTokens int64, segments []usageChartSegment) int64 {
	total := segmentTotal(segments)
	drawn := 0
	for _, segment := range segments {
		h := barHeight(segment.Value, maxTokens, maxHeight)
		if segment.Value > 0 && h < 2 {
			h = 2
		}
		if h <= 0 {
			continue
		}
		y := baseY - drawn - h
		drawBar(img, x, y, width, h, segment.Color)
		drawn += h
	}
	return total
}

func segmentTotal(segments []usageChartSegment) int64 {
	total := int64(0)
	for _, segment := range segments {
		total += segment.Value
	}
	return total
}

func barHeight(value, maxTokens int64, maxHeight int) int {
	if value <= 0 || maxTokens <= 0 || maxHeight <= 0 {
		return 0
	}
	return int(float64(value) / float64(maxTokens) * float64(maxHeight))
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

func drawSeriesLegend(img *image.RGBA, x, y int, legend []usageChartSegment) {
	for i, item := range legend {
		itemX := x + i*120
		drawFlatBar(img, itemX, y-12, 24, 10, item.Color)
		drawText(img, itemX+30, y, item.Label, color.RGBA{R: 229, G: 235, B: 229, A: 255})
	}
}

func drawStackedLegend(img *image.RGBA, x, y int, inputLegend, outputLegend []usageChartSegment, muted color.RGBA) {
	drawText(img, x, y, "input:", muted)
	drawSeriesLegend(img, x+70, y, inputLegend)
	drawText(img, x, y+28, "output:", muted)
	drawSeriesLegend(img, x+70, y+28, outputLegend)
}

func drawBar(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	drawFlatBar(img, x, y, w, h, c)
}

func drawFlatBar(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	draw.Draw(img, image.Rect(x, y, x+w, y+h), &image.Uniform{C: c}, image.Point{}, draw.Src)
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

func subByte(value, sub uint8) uint8 {
	if sub > value {
		return 0
	}
	return value - sub
}
