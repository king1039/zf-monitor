package main

import "testing"

func TestServiceMetadata(t *testing.T) {
	if serviceName != "ZFMonitorAgent" {
		t.Fatalf("serviceName = %q, want %q", serviceName, "ZFMonitorAgent")
	}
	if serviceDisplayName != "Stark monitor Agent" {
		t.Fatalf("serviceDisplayName = %q, want %q", serviceDisplayName, "Stark monitor Agent")
	}
}
