package power

import (
	"reflect"
	"testing"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

func kinds(k ...caps.PowerEventKind) []caps.PowerEventKind { return k }

func TestTrackerBatteryEvents(t *testing.T) {
	var tr tracker
	if got := tr.update(caps.PowerSample{OnBattery: false, IdleSeconds: -1}); got != nil {
		t.Fatalf("the first sample is a baseline, got %v", got)
	}
	if got := tr.update(caps.PowerSample{OnBattery: true}); !reflect.DeepEqual(got, kinds(caps.PowerOnBatt)) {
		t.Fatalf("unplugged: %v", got)
	}
	if got := tr.update(caps.PowerSample{OnBattery: true}); got != nil {
		t.Fatalf("no change: %v", got)
	}
	if got := tr.update(caps.PowerSample{OnBattery: false}); !reflect.DeepEqual(got, kinds(caps.PowerOnAC)) {
		t.Fatalf("plugged in: %v", got)
	}
}

func TestTrackerIdleEvents(t *testing.T) {
	var tr tracker
	tr.update(caps.PowerSample{IdleSeconds: 0})
	// Without a threshold there are no idle events.
	if got := tr.update(caps.PowerSample{IdleSeconds: 1000}); got != nil {
		t.Fatalf("no threshold: %v", got)
	}
	tr.setThreshold(60)
	if got := tr.update(caps.PowerSample{IdleSeconds: 30}); got != nil {
		t.Fatalf("below: %v", got)
	}
	if got := tr.update(caps.PowerSample{IdleSeconds: 61}); !reflect.DeepEqual(got, kinds(caps.PowerIdle)) {
		t.Fatalf("crossing: %v", got)
	}
	if got := tr.update(caps.PowerSample{IdleSeconds: 90}); got != nil {
		t.Fatalf("still idle: %v", got)
	}
	if got := tr.update(caps.PowerSample{IdleSeconds: 2}); !reflect.DeepEqual(got, kinds(caps.PowerActive)) {
		t.Fatalf("input: %v", got)
	}
	// An unknown idle time neither starts nor ends idleness.
	tr.update(caps.PowerSample{IdleSeconds: 70})
	if got := tr.update(caps.PowerSample{IdleSeconds: -1}); got != nil {
		t.Fatalf("unknown: %v", got)
	}
	tr.setThreshold(0)
	if got := tr.update(caps.PowerSample{IdleSeconds: 0}); got != nil {
		t.Fatalf("turned off: %v", got)
	}
}

func TestTrackerState(t *testing.T) {
	var tr tracker
	if st := tr.state(caps.PowerSample{IdleSeconds: -1}); st.IdleState != "unknown" {
		t.Errorf("%+v", st)
	}
	if st := tr.state(caps.PowerSample{IdleSeconds: 5}); st.IdleState != "active" {
		t.Errorf("%+v", st)
	}
	tr.setThreshold(10)
	if st := tr.state(caps.PowerSample{IdleSeconds: 11}); st.IdleState != "idle" {
		t.Errorf("%+v", st)
	}
	tr.push(caps.PowerLock)
	if st := tr.state(caps.PowerSample{IdleSeconds: 11}); st.IdleState != "locked" || !st.Locked {
		t.Errorf("locked: %+v", st)
	}
	tr.push(caps.PowerUnlock)
	if st := tr.state(caps.PowerSample{IdleSeconds: 1}); st.Locked {
		t.Errorf("unlocked: %+v", st)
	}
	if st := tr.state(caps.PowerSample{Locked: true, IdleSeconds: 1}); !st.Locked {
		t.Errorf("provider says locked: %+v", st)
	}
}
