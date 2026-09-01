package main

import "testing"

func TestServiceMetadata(t *testing.T) {
	if serviceName != "ZFMonitorAgent" {
		t.Fatalf("serviceName = %q, want %q", serviceName, "ZFMonitorAgent")
	}
	if serviceDisplayName != "ZF Monitor Agent" {
		t.Fatalf("serviceDisplayName = %q, want %q", serviceDisplayName, "ZF Monitor Agent")
	}
}
