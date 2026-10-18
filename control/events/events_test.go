package events

import "testing"

func TestPublishNumbersAndMarshals(t *testing.T) {
	b := New()
	e1 := b.Publish("a", map[string]int{"n": 1})
	e2 := b.Publish("b", "x")
	if e1.ID != 1 || e2.ID != 2 {
		t.Fatalf("ids = %d, %d", e1.ID, e2.ID)
	}
	if string(e1.Data) != `{"n":1}` || string(e2.Data) != `"x"` {
		t.Errorf("data = %s, %s", e1.Data, e2.Data)
	}
}

func TestSubscribeReplaysThenGoesLive(t *testing.T) {
	b := New()
	b.Publish("a", 1)
	b.Publish("a", 2)
	b.Publish("a", 3)
	missed, live, stop := b.Subscribe(1)
	defer stop()
	if len(missed) != 2 || missed[0].ID != 2 || missed[1].ID != 3 {
		t.Fatalf("missed = %+v", missed)
	}
	b.Publish("b", 4)
	if e := <-live; e.ID != 4 || e.Type != "b" {
		t.Fatalf("live = %+v", e)
	}
	all, _, stop2 := b.Subscribe(0)
	stop2()
	if len(all) != 4 {
		t.Fatalf("from the start: %d events", len(all))
	}
	none, _, stop3 := b.Subscribe(-1)
	stop3()
	if len(none) != 0 {
		t.Fatalf("a negative after replays %d events", len(none))
	}
}

func TestOnlyTheLastRetainedAreKept(t *testing.T) {
	b := New()
	for i := 0; i < Retained+10; i++ {
		b.Publish("a", i)
	}
	missed, _, stop := b.Subscribe(0)
	defer stop()
	if len(missed) != Retained || missed[0].ID != 11 {
		t.Fatalf("kept %d events from ID %d", len(missed), missed[0].ID)
	}
}

func TestStopClosesTheChannelOnce(t *testing.T) {
	b := New()
	_, live, stop := b.Subscribe(0)
	stop()
	stop()
	if _, ok := <-live; ok {
		t.Fatal("channel still open")
	}
	b.Publish("a", 1) // must not panic on the closed subscriber
}

func TestSlowSubscriberIsCutOffAndOthersAreNot(t *testing.T) {
	b := New()
	_, slow, stopSlow := b.Subscribe(0)
	defer stopSlow()
	_, fast, stopFast := b.Subscribe(0)
	defer stopFast()
	got := 0
	for i := 0; i < buffer+5; i++ {
		b.Publish("a", i)
		<-fast
		got++
	}
	n := 0
	for range slow { // closed once full, so this ends
		n++
	}
	if n != buffer {
		t.Errorf("slow subscriber got %d events, want %d before the cut", n, buffer)
	}
	if got != buffer+5 {
		t.Errorf("fast subscriber got %d", got)
	}
}
