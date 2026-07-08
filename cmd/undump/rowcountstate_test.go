package main

import "testing"

func TestRowcountState_UnknownTargetReturnsNil(t *testing.T) {
	state := newRowcountState()

	if got := state.get("prod-billing"); got != nil {
		t.Fatalf("expected nil for an untracked target, got %v", *got)
	}
}

func TestRowcountState_SetThenGetRoundTrips(t *testing.T) {
	state := newRowcountState()
	value := int64(1000)

	state.set("prod-billing", &value)
	got := state.get("prod-billing")

	if got == nil || *got != 1000 {
		t.Fatalf("expected 1000, got %v", got)
	}
}

func TestRowcountState_SetNilIsANoOp(t *testing.T) {
	state := newRowcountState()
	value := int64(1000)
	state.set("prod-billing", &value)

	state.set("prod-billing", nil)

	got := state.get("prod-billing")
	if got == nil || *got != 1000 {
		t.Fatalf("a nil set should not overwrite the previous value, got %v", got)
	}
}

func TestRowcountState_TracksTargetsIndependently(t *testing.T) {
	state := newRowcountState()
	a, b := int64(10), int64(20)

	state.set("target-a", &a)
	state.set("target-b", &b)

	if got := state.get("target-a"); got == nil || *got != 10 {
		t.Fatalf("target-a: expected 10, got %v", got)
	}
	if got := state.get("target-b"); got == nil || *got != 20 {
		t.Fatalf("target-b: expected 20, got %v", got)
	}
}
