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
		a       float64
		b       float64
		want    float64
		wantErr bool
	}{
		{1, 0, 0, true}, {1, 1, 1, false}, {1, -1, -1, false},
	}
	for _, c := range cases {
		got, err := Divide(c.a, c.b)
		if (err != nil) != c.wantErr {
			t.Errorf("Divide(%f, %f) = %v, want error: %v", c.a, c.b, err, c.wantErr)
		}

		if got != c.want {
			t.Errorf("Divide(%f, %f) = %v, want %v", c.a, c.b, got, c.want)
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
