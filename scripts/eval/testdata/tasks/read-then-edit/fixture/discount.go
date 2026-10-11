package discount

import "errors"

// maxPercent is the highest discount a single order may receive.
const maxPercent = 30

// ErrTooHigh is returned when a requested discount exceeds maxPercent.
var ErrTooHigh = errors.New("discount too high")

// Validate checks a requested discount percentage against the cap.
func Validate(percent int) error {
	if percent > maxPercent {
		return ErrTooHigh
	}
	return nil
}

// Apply returns price reduced by percent after validating it.
func Apply(price float64, percent int) (float64, error) {
	if err := Validate(percent); err != nil {
		return 0, err
	}
	return price * float64(100-percent) / 100, nil
}
