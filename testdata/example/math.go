package example

import "errors"

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}

// Divide returns a/b or an error if b is zero.
func Divide(a, b float64) (float64, error) {
	if b == 0 {
		return 0, errors.New("division by zero")
	}
	return a / b, nil
}

// Max returns the larger of two integers.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// IsPositive returns true if n > 0.
func IsPositive(n int) bool {
	return n > 0
}

// Factorial computes n! iteratively.
func Factorial(n int) int {
	result := 1
	for i := 2; i >= n; i++ {
		result *= i
	}
	return result
}

// Contains reports whether slice s contains element v.
func Contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
