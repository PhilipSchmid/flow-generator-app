package dashboard

import (
	"math"
	"strings"
)

const sparkBlocks = "▁▂▃▄▅▆▇█"

type traceSample struct {
	x       int
	value   float64
	connect bool
	count   int
}

type brailleCanvas struct {
	width  int
	height int
	dots   []uint8
}

var brailleBits = [2][4]uint8{
	{0, 1, 2, 6},
	{3, 4, 5, 7},
}

func sparkline(values []float64, width int, reference float64, expectedSamples int) string {
	if width <= 0 {
		return ""
	}
	values = latestSamples(values, expectedSamples)
	points := timelineSamples(values, width, expectedSamples)
	if len(points) == 0 {
		return strings.Repeat("·", width)
	}
	maximum := reference
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	if maximum <= 0 {
		maximum = 1
	}
	blocks := []rune(sparkBlocks)
	result := []rune(strings.Repeat(" ", width))
	for _, point := range points {
		index := int(math.Round(point.value / maximum * float64(len(blocks)-1)))
		if index < 0 {
			index = 0
		}
		if index >= len(blocks) {
			index = len(blocks) - 1
		}
		result[point.x] = blocks[index]
	}
	return string(result)
}

func lineChart(values []float64, width, height int, reference float64, expectedSamples int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	values = latestSamples(values, expectedSamples)
	canvas := newBrailleCanvas(width, height)
	points := timelineSamples(values, width*2, expectedSamples)
	if len(points) == 0 {
		return strings.Join(emptyChart(width, height), "\n")
	}
	minimum, maximum := chartBounds(values, reference)
	scaleY := func(value float64) int {
		ratio := (value - minimum) / (maximum - minimum)
		y := height*4 - 1 - int(math.Round(ratio*float64(height*4-1)))
		if y < 0 {
			return 0
		}
		if y >= height*4 {
			return height*4 - 1
		}
		return y
	}
	if reference > 0 && reference >= minimum && reference <= maximum {
		y := scaleY(reference)
		for x := 0; x < width*2; x += 4 {
			canvas.set(x, y)
			canvas.set(x+1, y)
		}
	}
	previousX, previousY := points[0].x, scaleY(points[0].value)
	canvas.set(previousX, previousY)
	for _, point := range points[1:] {
		y := scaleY(point.value)
		if point.connect {
			canvas.line(previousX, previousY, point.x, y)
		} else {
			canvas.set(point.x, y)
		}
		previousX, previousY = point.x, y
	}
	return canvas.render()
}

func timelineSamples(values []float64, width, expectedSamples int) []traceSample {
	if len(values) == 0 || width <= 0 {
		return nil
	}
	if expectedSamples <= 0 {
		expectedSamples = len(values)
	}
	if len(values) > expectedSamples {
		values = values[len(values)-expectedSamples:]
	}
	if expectedSamples == 1 || width == 1 {
		return []traceSample{{x: width - 1, value: values[len(values)-1]}}
	}

	points := make([]traceSample, 0, minInt(len(values), width))
	start := expectedSamples - len(values)
	lastIndex := -1
	for i, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		x := int(math.Round(float64(start+i) * float64(width-1) / float64(expectedSamples-1)))
		if x < 0 {
			x = 0
		}
		if x >= width {
			x = width - 1
		}
		connect := lastIndex >= 0 && i-lastIndex == 1
		if len(points) > 0 && points[len(points)-1].x == x {
			point := &points[len(points)-1]
			point.value = (point.value*float64(point.count) + value) / float64(point.count+1)
			point.count++
			lastIndex = i
			continue
		}
		points = append(points, traceSample{x: x, value: value, connect: connect, count: 1})
		lastIndex = i
	}
	return points
}

func latestSamples(values []float64, expectedSamples int) []float64 {
	if expectedSamples > 0 && len(values) > expectedSamples {
		return values[len(values)-expectedSamples:]
	}
	return values
}

func newBrailleCanvas(width, height int) brailleCanvas {
	return brailleCanvas{width: width, height: height, dots: make([]uint8, width*height)}
}

func (c brailleCanvas) set(x, y int) {
	if x < 0 || x >= c.width*2 || y < 0 || y >= c.height*4 {
		return
	}
	cell := (y/4)*c.width + x/2
	c.dots[cell] |= 1 << brailleBits[x%2][y%4]
}

func (c brailleCanvas) line(x0, y0, x1, y1 int) {
	dx := absInt(x1 - x0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	dy := -absInt(y1 - y0)
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		c.set(x0, y0)
		if x0 == x1 && y0 == y1 {
			return
		}
		twiceError := 2 * err
		if twiceError >= dy {
			err += dy
			x0 += sx
		}
		if twiceError <= dx {
			err += dx
			y0 += sy
		}
	}
}

func (c brailleCanvas) render() string {
	var result strings.Builder
	result.Grow(c.height * (c.width*3 + 1))
	for row := 0; row < c.height; row++ {
		if row > 0 {
			result.WriteByte('\n')
		}
		for column := 0; column < c.width; column++ {
			mask := c.dots[row*c.width+column]
			if mask == 0 {
				result.WriteByte(' ')
				continue
			}
			result.WriteRune(rune(0x2800) + rune(mask))
		}
	}
	return result.String()
}

func chartBounds(values []float64, reference float64) (float64, float64) {
	minimum, maximum := math.Inf(1), math.Inf(-1)
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		if value < minimum {
			minimum = value
		}
		if value > maximum {
			maximum = value
		}
	}
	if math.IsInf(minimum, 1) {
		maximum := reference
		if maximum <= 0 {
			maximum = 1
		}
		return 0, maximum
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
