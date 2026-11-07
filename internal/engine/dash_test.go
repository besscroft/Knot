package engine

import "testing"

func TestScaleProgress(t *testing.T) {
	cases := []struct {
		name             string
		down, total, exp int64
		want             int64
	}{
		{"total 与预估一致", 500, 1000, 1000, 500},
		{"total 与预估不符按比例折算", 250, 500, 1000, 500},
		{"折算不超预估上限", 900, 500, 400, 400},
		{"无预估时原样", 123, 456, 0, 123},
		{"total 未知时原样", 123, 0, 1000, 123},
	}
	for _, c := range cases {
		if got := scaleProgress(c.down, c.total, c.exp); got != c.want {
			t.Errorf("%s: scaleProgress(%d,%d,%d) = %d, want %d", c.name, c.down, c.total, c.exp, got, c.want)
		}
	}
}
