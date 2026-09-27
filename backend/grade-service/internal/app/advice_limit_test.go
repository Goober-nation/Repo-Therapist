package app

import "testing"

func TestAdviceLimit(t *testing.T) {
	cases := map[string]int{"": DefaultAdviceLimit, "5": 5, "0": 0, "-1": DefaultAdviceLimit, "abc": DefaultAdviceLimit}
	for v, want := range cases {
		t.Setenv("GRADE_ADVICE_LIMIT", v)
		if got := AdviceLimit(); got != want {
			t.Errorf("GRADE_ADVICE_LIMIT=%q: got %d, want %d", v, got, want)
		}
	}
}
