package dashboard

import (
	"math"
	"strings"
)

const sparkBlocks = "▁▂▃▄▅▆▇█"

func sparkline(values []float64, width int, reference float64) string {
	if width <= 0 {
		return ""
	}
	values = downsample(values, width)
	if len(values) == 0 {
		return strings.Repeat("·", width)
	}
	maximum := reference
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	if maximum <= 0 {
		return strings.Repeat("▁", len(values))
	}
	blocks := []rune(sparkBlocks)
	var result strings.Builder
	result.Grow(len(values) * 3)
	for _, value := range values {
		index := int(math.Round(value / maximum * float64(len(blocks)-1)))
		if index < 0 {
			index = 0
		}
		if index >= len(blocks) {
			index = len(blocks) - 1
		}
		result.WriteRune(blocks[index])
	}
	return result.String()
}

func downsample(values []float64, width int) []float64 {
	if len(values) <= width {
		return values
	}
	result := make([]float64, width)
	for i := range result {
		start := i * len(values) / width
		end := (i + 1) * len(values) / width
		if end <= start {
			end = start + 1
		}
		var sum float64
		for _, value := range values[start:end] {
			sum += value
		}
		result[i] = sum / float64(end-start)
	}
	return result
}
