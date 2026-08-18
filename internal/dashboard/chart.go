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
	values = fitSamples(values, width)
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

func lineChart(values []float64, width, height int, reference float64) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	values = fitSamples(values, width)
	if len(values) == 0 {
		return strings.Join(emptyChart(width, height), "\n")
	}
	minimum, maximum := chartBounds(values, reference)
	grid := make([][]rune, height)
	for row := range grid {
		grid[row] = []rune(strings.Repeat(" ", width))
	}
	scaleRow := func(value float64) int {
		ratio := (value - minimum) / (maximum - minimum)
		row := height - 1 - int(math.Round(ratio*float64(height-1)))
		if row < 0 {
			return 0
		}
		if row >= height {
			return height - 1
		}
		return row
	}
	if reference > 0 && reference >= minimum && reference <= maximum {
		row := scaleRow(reference)
		for column := range width {
			grid[row][column] = '┄'
		}
	}
	previousRow := -1
	for column, value := range values {
		row := scaleRow(value)
		if previousRow == row {
			grid[row][column] = '─'
		} else {
			grid[row][column] = '●'
			if previousRow >= 0 {
				top, bottom := row, previousRow
				if top > bottom {
					top, bottom = bottom, top
				}
				for bridge := top + 1; bridge < bottom; bridge++ {
					grid[bridge][column] = '│'
				}
			}
		}
		previousRow = row
	}
	lines := make([]string, height)
	for row := range grid {
		lines[row] = string(grid[row])
	}
	return strings.Join(lines, "\n")
}

func chartBounds(values []float64, reference float64) (float64, float64) {
	minimum, maximum := values[0], values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	if reference > 0 {
		if reference < minimum {
			minimum = reference
		}
		if reference > maximum {
			maximum = reference
		}
	}
	span := maximum - minimum
	if span <= 0 {
		span = math.Abs(maximum) * 0.1
		if span < 1 {
			span = 1
		}
	}
	padding := span * 0.1
	minimum -= padding
	maximum += padding
	if minimum < 0 {
		minimum = 0
	}
	if maximum <= minimum {
		maximum = minimum + 1
	}
	return minimum, maximum
}

func emptyChart(width, height int) []string {
	lines := make([]string, height)
	for row := range lines {
		if row == height-1 {
			lines[row] = strings.Repeat("·", width)
		} else {
			lines[row] = strings.Repeat(" ", width)
		}
	}
	return lines
}

func fitSamples(values []float64, width int) []float64 {
	if len(values) == 0 || width <= 0 {
		return nil
	}
	if len(values) < width {
		result := make([]float64, width)
		for i := range result {
			result[i] = values[i*len(values)/width]
		}
		return result
	}
	return downsample(values, width)
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
