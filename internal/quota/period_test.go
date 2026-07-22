package quota

import (
	"errors"
	"math"
	"testing"
)

func TestResolveLegacyDayCompatibilityAndConflict(t *testing.T) {
	legacy := 10.1
	day := 11.0
	patch, err := ResolveLegacyDay(&legacy, &PeriodSpendingLimitsPatch{Day: &day})
	if err != nil || patch == nil || patch.Day == nil || *patch.Day != 11 {
		t.Fatalf("same normalized day = %#v, err=%v", patch, err)
	}
	conflict := 12.0
	if _, err := ResolveLegacyDay(&legacy, &PeriodSpendingLimitsPatch{Day: &conflict}); !errors.Is(err, ErrPeriodDayLegacyConflict) {
		t.Fatalf("conflict err = %v, want ErrPeriodDayLegacyConflict", err)
	}
}

func TestNormalizeWholeUSDRejectsInvalidAndCeilsPositive(t *testing.T) {
	for _, value := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NormalizeWholeUSD(value); !errors.Is(err, ErrInvalidSpendingLimit) {
			t.Fatalf("NormalizeWholeUSD(%v) err=%v, want invalid", value, err)
		}
	}
	if got, err := NormalizeWholeUSD(10.01); err != nil || got != 11 {
		t.Fatalf("NormalizeWholeUSD = %v, %v; want 11,nil", got, err)
	}
}
