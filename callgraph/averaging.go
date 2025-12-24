package callgraph

import "time"

const DEFAULT_SMA_WINDOW_SIZE = 10
const DEFAULT_EMA_ALPHA = 0.3

// AveragingCalculator defines the interface for execution time averaging
type AveragingCalculator interface {
	// Add adds a new execution time sample and returns the current average
	Add(executionTime time.Duration) time.Duration

	// Average returns the current average
	Average() time.Duration

	// Reset resets the calculator
	Reset()

	// Count returns the number of samples
	Count() int
}

// SMACalculator implements Simple Moving Average
type SMACalculator struct {
	window     []time.Duration
	windowIdx  int
	windowSize int
	sum        time.Duration
	count      int
	full       bool
}

// NewSMACalculator creates a new SMA calculator with the given window size
func NewSMACalculator(config any) *SMACalculator {
	c, ok := config.(*SMAConfig)
	if !ok {
		panic("invalid config type for SMA")
	}
	windowSize := c.WindowSize
	if windowSize <= 0 {
		windowSize = DEFAULT_SMA_WINDOW_SIZE
	}
	return &SMACalculator{
		window:     make([]time.Duration, windowSize),
		windowSize: windowSize,
	}
}

// Add adds a new sample and returns the current SMA
func (s *SMACalculator) Add(executionTime time.Duration) time.Duration {
	// Remove old value from sum if window is full
	if s.full {
		s.sum -= s.window[s.windowIdx]
	}

	// Add new value
	s.window[s.windowIdx] = executionTime
	s.sum += executionTime
	s.windowIdx = (s.windowIdx + 1) % s.windowSize

	if !s.full {
		s.count++
		if s.windowIdx == 0 {
			s.full = true
		}
	}

	return s.Average()
}

// Average returns the current SMA
func (s *SMACalculator) Average() time.Duration {
	if s.count == 0 {
		return 0
	}
	// When window is full, divide by windowSize; otherwise divide by count
	divisor := s.count
	if s.full {
		divisor = s.windowSize
	}
	return s.sum / time.Duration(divisor)
}

// Reset resets the calculator
func (s *SMACalculator) Reset() {
	s.window = make([]time.Duration, s.windowSize)
	s.windowIdx = 0
	s.sum = 0
	s.count = 0
	s.full = false
}

// Count returns the number of samples added
func (s *SMACalculator) Count() int {
	return s.count
}

// EMACalculator implements Exponential Moving Average
type EMACalculator struct {
	alpha   float64
	average float64
	count   int
}

// NewEMACalculator creates a new EMA calculator with the given alpha (smoothing factor)
// alpha should be between 0 and 1, where higher values give more weight to recent data
func NewEMACalculator(config any) *EMACalculator {
	c, ok := config.(*EMAConfig)
	if !ok {
		panic("invalid config type for EMA")
	}
	alpha := c.Alpha
	if alpha <= 0 || alpha > 1 {
		alpha = DEFAULT_EMA_ALPHA
	}
	return &EMACalculator{
		alpha: alpha,
	}
}

// Add adds a new sample and returns the current EMA
func (e *EMACalculator) Add(executionTime time.Duration) time.Duration {
	value := float64(executionTime)
	if e.count == 0 {
		e.average = value
	} else {
		// EMA = alpha * current + (1 - alpha) * previous_EMA
		e.average = e.alpha*value + (1-e.alpha)*e.average
	}
	e.count++
	return time.Duration(e.average)
}

// Average returns the current EMA
func (e *EMACalculator) Average() time.Duration {
	return time.Duration(e.average)
}

// Reset resets the calculator
func (e *EMACalculator) Reset() {
	e.average = 0
	e.count = 0
}

// Count returns the number of samples added
func (e *EMACalculator) Count() int {
	return e.count
}

// NewAveragingCalculator creates an averaging calculator based on the config
func NewAveragingCalculator(method AveragingMethod, config any) AveragingCalculator {
	switch method {
	case ExponentialMovingAverage:
		return NewEMACalculator(config)
	case SimpleMovingAverage:
		fallthrough
	default:
		return NewSMACalculator(config)
	}
}
