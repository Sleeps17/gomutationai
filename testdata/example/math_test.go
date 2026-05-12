package example

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Errorf("Add(2,3) = %d, want 5", got)
	}
	if got := Add(-1, 1); got != 0 {
		t.Errorf("Add(-1,1) = %d, want 0", got)
	}
}

func TestDivide(t *testing.T) {
	cases := []struct {
		a, b float64
		want float64
		err  bool
	}{
		{10, 2, 5, false},
		{1, 0, 0, true},
		{1, 1, 1, false},
		{1, -1, -1, false},
	}
	for _, c := range cases {
		got, err := Divide(c.a, c.b)
		if c.err {
			if err == nil {
				t.Errorf("Divide(%v, %v) want error: true", c.a, c.b)
			}
			continue
		}
		if err != nil {
			t.Errorf("Divide(%v, %v) = %v, want error: false", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("Divide(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestMax(t *testing.T) {
	if got := Max(3, 5); got != 5 {
		t.Errorf("Max(3,5) = %d, want 5", got)
	}
	if got := Max(7, 2); got != 7 {
		t.Errorf("Max(7,2) = %d, want 7", got)
	}
}

func TestIsPositive(t *testing.T) {
	if !IsPositive(1) {
		t.Error("IsPositive(1) should be true")
	}
	if IsPositive(-1) {
		t.Error("IsPositive(-1) should be false")
	}
	if IsPositive(0) {
		t.Error("IsPositive(0) should be false")
	}
}

func TestFactorial(t *testing.T) {
	cases := []struct{ n, want int }{
		{0, 1}, {1, 1}, {5, 120}, {6, 720},
	}
	for _, c := range cases {
		if got := Factorial(c.n); got != c.want {
			t.Errorf("Factorial(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestContains(t *testing.T) {
}
