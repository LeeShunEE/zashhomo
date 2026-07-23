package svc

import (
	"testing"
)

func TestConfig(t *testing.T) {
	// Test with empty exePath
	c := config("")
	if c.Name != serviceName {
		t.Errorf("name = %q, want %q", c.Name, serviceName)
	}
	if c.DisplayName != serviceDisplayName {
		t.Errorf("displayName = %q, want %q", c.DisplayName, serviceDisplayName)
	}
	if len(c.Arguments) != 1 || c.Arguments[0] != "run" {
		t.Errorf("arguments = %v, want [run]", c.Arguments)
	}

	// Test with exePath
	c = config("/path/to/zashhomo")
	if c.Executable != "/path/to/zashhomo" {
		t.Errorf("executable = %q, want /path/to/zashhomo", c.Executable)
	}
}

func TestPlatform(t *testing.T) {
	p := Platform()
	if p == "" {
		t.Error("Platform returned empty string")
	}
}

func TestStateDefaults(t *testing.T) {
	// Just verify the State struct can be created and zero-valued
	s := State{}
	if s.Installed || s.Running {
		t.Error("State should default to false")
	}
}

func TestGenericStateNoService(t *testing.T) {
	// On a system without the service installed, this should return a zero State
	// We can't make many assumptions about the test environment
	s := genericState()
	// Just verify it doesn't panic
	_ = s
}

func TestNewService(t *testing.T) {
	// Test creating a service wrapper
	s, p, err := newService(nil, "")
	if err != nil {
		t.Fatalf("newService failed: %v", err)
	}
	if s == nil {
		t.Error("service is nil")
	}
	if p == nil {
		t.Error("program is nil")
	}
}

func TestNewServiceWithExePath(t *testing.T) {
	s, _, err := newService(nil, "/custom/path/zashhomo")
	if err != nil {
		t.Fatalf("newService with exePath failed: %v", err)
	}
	if s == nil {
		t.Error("service is nil")
	}
}
