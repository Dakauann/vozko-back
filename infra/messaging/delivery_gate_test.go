package queue

import (
	"testing"
	"time"
)

func TestDrainWaitsForTheDeliveryInProgress(t *testing.T) {
	var g deliveryGate
	if !g.enter() {
		t.Fatal("an open gate admits a delivery")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		g.leave()
	}()

	started := time.Now()
	if !g.drain(time.Second) {
		t.Fatal("the delivery finished inside the timeout")
	}
	if time.Since(started) < 40*time.Millisecond {
		t.Fatal("drain returned before the delivery finished")
	}
}

func TestADrainingGateTurnsNewDeliveriesAway(t *testing.T) {
	var g deliveryGate
	g.drain(time.Millisecond)
	if g.enter() {
		t.Fatal("a delivery arriving during shutdown goes back to the queue")
	}
}

func TestDrainGivesUpAfterItsTimeout(t *testing.T) {
	var g deliveryGate
	g.enter()
	if g.drain(20 * time.Millisecond) {
		t.Fatal("a stuck delivery cannot hold the shutdown forever")
	}
}
