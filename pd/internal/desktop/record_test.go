package desktop

import (
	"testing"
)

func TestParseFreezeIntervals_NoFreezes(t *testing.T) {
	result := parseFreezeIntervals("", 10.0)
	if len(result) != 0 {
		t.Fatalf("expected 0 intervals, got %d", len(result))
	}
}

func TestParseFreezeIntervals_SingleFreeze(t *testing.T) {
	output := `[freezedetect @ 0x1234] freeze_start: 2.5
[freezedetect @ 0x1234] freeze_end: 5.0`

	result := parseFreezeIntervals(output, 10.0)
	if len(result) != 1 {
		t.Fatalf("expected 1 interval, got %d", len(result))
	}
	if result[0].Start != 2.5 || result[0].End != 5.0 {
		t.Fatalf("expected [2.5, 5.0], got [%f, %f]", result[0].Start, result[0].End)
	}
}

func TestParseFreezeIntervals_OverlappingFreezes_Merged(t *testing.T) {
	output := `[freezedetect @ 0x1234] freeze_start: 1.0
[freezedetect @ 0x1234] freeze_end: 3.0
[freezedetect @ 0x1234] freeze_start: 2.5
[freezedetect @ 0x1234] freeze_end: 5.0`

	result := parseFreezeIntervals(output, 10.0)
	if len(result) != 1 {
		t.Fatalf("expected 1 merged interval, got %d: %+v", len(result), result)
	}
	if result[0].Start != 1.0 || result[0].End != 5.0 {
		t.Fatalf("expected [1.0, 5.0], got [%f, %f]", result[0].Start, result[0].End)
	}
}

func TestParseFreezeIntervals_UnterminatedFreeze_ExtendsToEnd(t *testing.T) {
	output := `[freezedetect @ 0x1234] freeze_start: 7.0`

	result := parseFreezeIntervals(output, 10.0)
	if len(result) != 1 {
		t.Fatalf("expected 1 interval, got %d", len(result))
	}
	if result[0].Start != 7.0 || result[0].End != 10.0 {
		t.Fatalf("expected [7.0, 10.0], got [%f, %f]", result[0].Start, result[0].End)
	}
}

func TestParseFreezeIntervals_TinyIntervals_Filtered(t *testing.T) {
	output := `[freezedetect @ 0x1234] freeze_start: 1.0
[freezedetect @ 0x1234] freeze_end: 1.01
[freezedetect @ 0x1234] freeze_start: 3.0
[freezedetect @ 0x1234] freeze_end: 5.0`

	result := parseFreezeIntervals(output, 10.0)
	if len(result) != 1 {
		t.Fatalf("expected 1 interval (tiny one filtered), got %d: %+v", len(result), result)
	}
	if result[0].Start != 3.0 || result[0].End != 5.0 {
		t.Fatalf("expected [3.0, 5.0], got [%f, %f]", result[0].Start, result[0].End)
	}
}
