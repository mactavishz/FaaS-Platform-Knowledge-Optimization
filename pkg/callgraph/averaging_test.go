package callgraph

import (
	"testing"
	"time"
)

func TestSMACalculator(t *testing.T) {
	sma := NewSMACalculator(3)

	// Add first value
	avg := sma.Add(100 * time.Millisecond)
	if avg != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", avg)
	}

	// Add second value
	avg = sma.Add(200 * time.Millisecond)
	if avg != 150*time.Millisecond {
		t.Errorf("expected 150ms, got %v", avg)
	}

	// Add third value
	avg = sma.Add(300 * time.Millisecond)
	if avg != 200*time.Millisecond {
		t.Errorf("expected 200ms, got %v", avg)
	}

	// Add fourth value (should push out first)
	avg = sma.Add(400 * time.Millisecond)
	// Window: [200, 300, 400] -> avg = 300
	if avg != 300*time.Millisecond {
		t.Errorf("expected 300ms, got %v", avg)
	}

	if sma.Count() != 3 {
		t.Errorf("expected count 3 (window size), got %d", sma.Count())
	}
}

func TestSMACalculatorReset(t *testing.T) {
	sma := NewSMACalculator(5)
	sma.Add(100 * time.Millisecond)
	sma.Add(200 * time.Millisecond)
	sma.Reset()

	if sma.Count() != 0 {
		t.Errorf("expected count 0 after reset, got %d", sma.Count())
	}

	if sma.Average() != 0 {
		t.Errorf("expected average 0 after reset, got %v", sma.Average())
	}
}

func TestSMACalculatorDefaultWindow(t *testing.T) {
	// Invalid window size should default to 10
	sma := NewSMACalculator(0)
	if sma.windowSize != 10 {
		t.Errorf("expected default window size 10, got %d", sma.windowSize)
	}

	sma = NewSMACalculator(-5)
	if sma.windowSize != 10 {
		t.Errorf("expected default window size 10, got %d", sma.windowSize)
	}
}

func TestEMACalculator(t *testing.T) {
	ema := NewEMACalculator(0.5)

	// First value is the initial EMA
	avg := ema.Add(100 * time.Millisecond)
	if avg != 100*time.Millisecond {
		t.Errorf("expected 100ms, got %v", avg)
	}

	// Second value: EMA = 0.5 * 200 + 0.5 * 100 = 150
	avg = ema.Add(200 * time.Millisecond)
	if avg != 150*time.Millisecond {
		t.Errorf("expected 150ms, got %v", avg)
	}

	// Third value: EMA = 0.5 * 300 + 0.5 * 150 = 225
	avg = ema.Add(300 * time.Millisecond)
	if avg != 225*time.Millisecond {
		t.Errorf("expected 225ms, got %v", avg)
	}
}

func TestEMACalculatorReset(t *testing.T) {
	ema := NewEMACalculator(0.3)
	ema.Add(100 * time.Millisecond)
	ema.Add(200 * time.Millisecond)
	ema.Reset()

	if ema.Count() != 0 {
		t.Errorf("expected count 0 after reset, got %d", ema.Count())
	}

	if ema.Average() != 0 {
		t.Errorf("expected average 0 after reset, got %v", ema.Average())
	}
}

func TestEMACalculatorDefaultAlpha(t *testing.T) {
	// Invalid alpha should default to 0.3
	ema := NewEMACalculator(0)
	if ema.alpha != 0.3 {
		t.Errorf("expected default alpha 0.3, got %f", ema.alpha)
	}

	ema = NewEMACalculator(-0.5)
	if ema.alpha != 0.3 {
		t.Errorf("expected default alpha 0.3, got %f", ema.alpha)
	}

	ema = NewEMACalculator(1.5)
	if ema.alpha != 0.3 {
		t.Errorf("expected default alpha 0.3, got %f", ema.alpha)
	}
}

func TestNewAveragingCalculator(t *testing.T) {
	// Test SMA creation
	configSMA := Config{
		AveragingMethod: SimpleMovingAverage,
		SMAWindowSize:   5,
	}
	sma := NewAveragingCalculator(configSMA)
	if _, ok := sma.(*SMACalculator); !ok {
		t.Error("expected SMACalculator")
	}

	// Test EMA creation
	configEMA := Config{
		AveragingMethod: ExponentialMovingAverage,
		EMAAlpha:        0.5,
	}
	ema := NewAveragingCalculator(configEMA)
	if _, ok := ema.(*EMACalculator); !ok {
		t.Error("expected EMACalculator")
	}
}

func TestAveragingCalculatorInterface(t *testing.T) {
	// Verify interface compliance
	var _ AveragingCalculator = (*SMACalculator)(nil)
	var _ AveragingCalculator = (*EMACalculator)(nil)
}
