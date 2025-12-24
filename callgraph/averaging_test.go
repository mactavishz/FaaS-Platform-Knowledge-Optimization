package callgraph

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSMACalculator(t *testing.T) {
	sma := NewSMACalculator(&SMAConfig{WindowSize: 3})

	// Add first value
	avg := sma.Add(100 * time.Millisecond)
	assert.Equal(t, 100*time.Millisecond, avg)

	// Add second value
	avg = sma.Add(200 * time.Millisecond)
	assert.Equal(t, 150*time.Millisecond, avg)

	// Add third value
	avg = sma.Add(300 * time.Millisecond)
	assert.Equal(t, 200*time.Millisecond, avg)

	// Add fourth value (should push out first)
	avg = sma.Add(400 * time.Millisecond)
	// Window: [200, 300, 400] -> avg = 300
	assert.Equal(t, 300*time.Millisecond, avg)

	assert.Equal(t, 3, sma.Count())
}

func TestSMACalculatorReset(t *testing.T) {
	sma := NewSMACalculator(&SMAConfig{WindowSize: 3})
	sma.Add(100 * time.Millisecond)
	sma.Add(200 * time.Millisecond)
	sma.Reset()

	assert.Equal(t, 0, sma.Count())
	assert.Equal(t, time.Duration(0), sma.Average())
}

func TestSMACalculatorDefaultWindow(t *testing.T) {
	// Invalid window size should default to 10
	sma := NewSMACalculator(&SMAConfig{WindowSize: 0})
	assert.Equal(t, 10, sma.windowSize)

	sma = NewSMACalculator(&SMAConfig{WindowSize: -5})
	assert.Equal(t, 10, sma.windowSize)
}

// TestSMACalculatorWindowBehavior tests the fixed SMA calculation
func TestSMACalculatorWindowBehavior(t *testing.T) {
	sma := NewSMACalculator(&SMAConfig{WindowSize: 3})

	// Add values: 100, 200, 300
	sma.Add(100 * time.Millisecond)
	sma.Add(200 * time.Millisecond)
	sma.Add(300 * time.Millisecond)

	// Average should be (100+200+300)/3 = 200
	avg := sma.Average()
	assert.Equal(t, 200*time.Millisecond, avg)

	// Add fourth value: 600 (pushes out 100)
	// Window now: [200, 300, 600]
	sma.Add(600 * time.Millisecond)
	avg = sma.Average()
	// Average should be (200+300+600)/3 = 366.666...
	expected := 366666666 * time.Nanosecond
	assert.Equal(t, expected, avg)

	// Verify count stays at window size when full
	assert.Equal(t, 3, sma.Count())
}

func TestEMACalculator(t *testing.T) {
	ema := NewEMACalculator(&EMAConfig{Alpha: 0.5})

	// First value is the initial EMA
	avg := ema.Add(100 * time.Millisecond)
	assert.Equal(t, 100*time.Millisecond, avg)

	// Second value: EMA = 0.5 * 200 + 0.5 * 100 = 150
	avg = ema.Add(200 * time.Millisecond)
	assert.Equal(t, 150*time.Millisecond, avg)

	// Third value: EMA = 0.5 * 300 + 0.5 * 150 = 225
	avg = ema.Add(300 * time.Millisecond)
	assert.Equal(t, 225*time.Millisecond, avg)
}

func TestEMACalculatorReset(t *testing.T) {
	ema := NewEMACalculator(&EMAConfig{Alpha: 0.5})
	ema.Add(100 * time.Millisecond)
	ema.Add(200 * time.Millisecond)
	ema.Reset()

	assert.Equal(t, 0, ema.Count())
	assert.Equal(t, time.Duration(0), ema.Average())
}

func TestEMACalculatorDefaultAlpha(t *testing.T) {
	// Invalid alpha should default to 0.3
	ema := NewEMACalculator(&EMAConfig{Alpha: 0})
	assert.Equal(t, 0.3, ema.alpha)

	ema = NewEMACalculator(&EMAConfig{Alpha: -0.5})
	assert.Equal(t, 0.3, ema.alpha)

	ema = NewEMACalculator(&EMAConfig{Alpha: 1.5})
	assert.Equal(t, 0.3, ema.alpha)
}

func TestNewAveragingCalculator(t *testing.T) {
	sma := NewAveragingCalculator(SimpleMovingAverage, &SMAConfig{WindowSize: 3})
	assert.IsType(t, &SMACalculator{}, sma)

	ema := NewAveragingCalculator(ExponentialMovingAverage, &EMAConfig{Alpha: 0.5})
	assert.IsType(t, &EMACalculator{}, ema)
}

func TestAveragingCalculatorInterface(t *testing.T) {
	// Verify interface compliance
	var _ AveragingCalculator = (*SMACalculator)(nil)
	var _ AveragingCalculator = (*EMACalculator)(nil)
}

// TestSMACalculatorFullWindowDivision tests the fix for correct division when window is full
func TestSMACalculatorFullWindowDivision(t *testing.T) {
	sma := NewSMACalculator(&SMAConfig{WindowSize: 2})

	// Add first value
	sma.Add(100 * time.Millisecond)
	avg := sma.Average()
	// Average: 100/1 = 100
	assert.Equal(t, 100*time.Millisecond, avg)

	// Add second value (window now full)
	sma.Add(200 * time.Millisecond)
	avg = sma.Average()
	// Average: (100+200)/2 = 150
	assert.Equal(t, 150*time.Millisecond, avg)

	// Add third value (pushes out first)
	// Window: [200, 300]
	sma.Add(300 * time.Millisecond)
	avg = sma.Average()
	// Average: (200+300)/2 = 250 (should divide by windowSize=2, not count)
	assert.Equal(t, 250*time.Millisecond, avg)

	// Verify count behavior
	assert.Equal(t, 2, sma.Count())
}

// TestSMACalculatorLargeWindow tests SMA with larger window
func TestSMACalculatorLargeWindow(t *testing.T) {
	sma := NewSMACalculator(&SMAConfig{WindowSize: 5})

	values := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
	}

	// Fill the window
	for _, v := range values {
		sma.Add(v)
	}

	// Average: (100+200+300+400+500)/5 = 300
	avg := sma.Average()
	assert.Equal(t, 300*time.Millisecond, avg)

	// Add one more to push out the first
	// Window: [200, 300, 400, 500, 600]
	sma.Add(600 * time.Millisecond)
	avg = sma.Average()
	// Average: (200+300+400+500+600)/5 = 400
	assert.Equal(t, 400*time.Millisecond, avg)
}
