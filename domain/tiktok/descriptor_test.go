package tiktok

import (
	"reflect"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

func TestDescriptorNamesTheRegisteredChannel(t *testing.T) {
	d := Descriptor()
	if d.Kind != channel.KindTikTok || d.EntryType != shared.EntryTypeTikTok {
		t.Fatalf("kind %q entry %q", d.Kind, d.EntryType)
	}
	if string(d.Kind) != string(d.EntryType) {
		t.Fatalf("the channel kind and the entry type must be the same word: %q vs %q", d.Kind, d.EntryType)
	}
	if !reflect.DeepEqual(d.Capabilities, ChannelCapabilities()) {
		t.Fatal("the descriptor must carry the one capability set the adapter and the window use")
	}
	if d.Capabilities.OutboundBudget != OutboundBudget || d.Capabilities.OutboundWindow != MessagingWindow {
		t.Fatalf("window %v budget %d", d.Capabilities.OutboundWindow, d.Capabilities.OutboundBudget)
	}
}
