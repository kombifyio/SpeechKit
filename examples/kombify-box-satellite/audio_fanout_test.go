package main

import "testing"

func TestAudioFanoutReportsLossAndCopiesInput(t *testing.T) {
	var fanout audioFanout
	subscription := fanout.subscribe(1)
	input := []byte{1, 2, 3, 4}
	fanout.publish(input)
	input[0] = 99
	fanout.publish([]byte{5, 6, 7, 8})

	if got := subscription.DroppedFrames(); got != 1 {
		t.Fatalf("dropped frames = %d, want 1", got)
	}
	got := <-subscription.frames
	if got[0] != 1 {
		t.Fatalf("published frame aliases capture callback memory: %v", got)
	}
	fanout.unsubscribe(subscription.frames)
}

func TestAudioFanoutTracksLossPerSubscriber(t *testing.T) {
	var fanout audioFanout
	fast := fanout.subscribe(2)
	slow := fanout.subscribe(1)
	fanout.publish([]byte{1, 2})
	fastFrame := <-fast.frames
	fastFrame[0] = 99
	if slowFrame := <-slow.frames; slowFrame[0] != 1 {
		t.Fatalf("subscriber frames alias each other: %v", slowFrame)
	}
	fanout.publish([]byte{2})
	fanout.publish([]byte{3})

	if got := fast.DroppedFrames(); got != 0 {
		t.Fatalf("fast subscriber dropped frames = %d, want 0", got)
	}
	if got := slow.DroppedFrames(); got != 1 {
		t.Fatalf("slow subscriber dropped frames = %d, want 1", got)
	}
	fanout.unsubscribe(fast.frames)
	fanout.unsubscribe(slow.frames)
}

func TestAudioFanoutMarksEveryPlaybackOverlap(t *testing.T) {
	var fanout audioFanout
	preexisting := fanout.subscribe(2)
	fanout.beginPlayback()
	if !preexisting.PlaybackContaminated() {
		t.Fatal("subscription opened before playback was not marked contaminated")
	}
	fanout.publish([]byte{9, 9, 9, 9})
	overlapping := fanout.subscribe(2)
	if !overlapping.PlaybackContaminated() {
		t.Fatal("subscription opened during playback was not marked contaminated")
	}
	fanout.endPlayback()
	fanout.unsubscribe(preexisting.frames)
	fanout.unsubscribe(overlapping.frames)

	clean := fanout.subscribe(1)
	if clean.PlaybackContaminated() {
		t.Fatal("post-playback subscription remained contaminated")
	}
	fanout.unsubscribe(clean.frames)
}
