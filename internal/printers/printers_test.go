package printers

import (
	"testing"
)


func sample() []Printer {
	return []Printer{
		{ID: "usbprint:usbprint\\1fc9_2016\\a1", Name: "POS-80 (Copy 1)", Port: "USB002", Driver: "XPrinter XP-80C", Connected: true},
		{ID: "usbprint:usbprint\\04b8_0e15\\b2", Name: "Kitchen", Port: "USB003", Driver: "EPSON TM-T88V", Connected: true},
		{ID: "usbprint:usbprint\\1fc9_2016\\c3", Name: "POS-80", Port: "USB001", Driver: "XPrinter XP-80C", Stale: true},
	}
}

func TestResolveUSBUsesIdentityAfterQueueRename(t *testing.T) {
	job := Job{Transport: TransportUSB, PrinterID: "usbprint:usbprint\\1fc9_2016\\a1", DeviceID: "POS-80"}

	target, rebound := resolveUSB(job, sample())
	if target == nil {
		t.Fatal("expected the printer to be resolved by identity")
	}
	if target.Name != "POS-80 (Copy 1)" {
		t.Fatalf("resolved %q, want the renamed queue", target.Name)
	}
	if rebound {
		t.Fatal("an identity hit means nothing was rebound")
	}
}

func TestResolveUSBReportsReboundWhenOnlyTheNameMatches(t *testing.T) {
	job := Job{Transport: TransportUSB, PrinterID: "usbprint:usbprint\\gone", DeviceID: "Kitchen"}

	target, rebound := resolveUSB(job, sample())
	if target == nil || target.Name != "Kitchen" {
		t.Fatalf("resolved %v, want Kitchen", target)
	}
	if !rebound {
		t.Fatal("a name match after a missing identity must report rebound")
	}
}

func TestResolveUSBFallsBackToPort(t *testing.T) {
	job := Job{Transport: TransportUSB, DeviceID: "USB003"}

	target, rebound := resolveUSB(job, sample())
	if target == nil || target.Port != "USB003" {
		t.Fatalf("resolved %v, want the USB003 queue", target)
	}
	if !rebound {
		t.Fatal("a port match must report rebound")
	}
}

func TestResolveUSBAdoptsTheOnlyConnectedPrinter(t *testing.T) {
	onlyOne := []Printer{
		{ID: "usbprint:x", Name: "New printer", Port: "USB010", Connected: true},
	}
	job := Job{Transport: TransportUSB, PrinterID: "usbprint:old", DeviceID: "Old printer"}

	target, rebound := resolveUSB(job, onlyOne)
	if target == nil || target.Name != "New printer" {
		t.Fatalf("resolved %v, want the only connected printer", target)
	}
	if !rebound {
		t.Fatal("adopting a different printer must report rebound")
	}
}

func TestResolveUSBRefusesWhenAmbiguous(t *testing.T) {
	job := Job{Transport: TransportUSB, PrinterID: "usbprint:gone", DeviceID: "Missing"}
	if target, _ := resolveUSB(job, sample()); target != nil {
		t.Fatalf("resolved %q, want no guess with two connected printers", target.Name)
	}
}

func TestResolveUSBEmptyList(t *testing.T) {
	if target, rebound := resolveUSB(Job{Transport: TransportUSB}, nil); target != nil || rebound {
		t.Fatal("an empty list cannot resolve a printer")
	}
}

func TestTransportValid(t *testing.T) {
	for _, transport := range []Transport{TransportNetwork, TransportUSB, TransportBluetooth} {
		if !transport.Valid() {
			t.Fatalf("%s should be valid", transport)
		}
	}
	if Transport("SERIAL").Valid() {
		t.Fatal("SERIAL is not a transport this agent accepts")
	}
}

func TestReportDefaults(t *testing.T) {
	report := Report{Success: true, BytesWritten: 10, Via: "tcp", DurationMs: 1}
	if !report.Success || report.BytesWritten != 10 {
		t.Fatal("report fields must round-trip")
	}
}
