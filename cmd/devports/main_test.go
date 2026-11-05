package main

import "testing"

func TestPortsForMain(t *testing.T) {
	for _, id := range []string{"", "main"} {
		api, web, err := portsFor(id, false)
		if err != nil {
			t.Fatalf("portsFor(%q): %v", id, err)
		}
		if api != 8080 || web != 5173 {
			t.Errorf("portsFor(%q) = %d/%d, want 8080/5173", id, api, web)
		}
	}
}

func TestPortsForSlotDeterministicAndInRange(t *testing.T) {
	api1, web1, err := portsFor("agent1", false)
	if err != nil {
		t.Fatal(err)
	}
	api2, web2, err := portsFor("agent1", false)
	if err != nil {
		t.Fatal(err)
	}
	if api1 != api2 || web1 != web2 {
		t.Errorf("not deterministic: %d/%d vs %d/%d", api1, web1, api2, web2)
	}
	if api1 < 8081 || api1 > 8179 {
		t.Errorf("api port %d out of range 8081-8179", api1)
	}
	if web1 < 5174 || web1 > 5272 {
		t.Errorf("web port %d out of range 5174-5272", web1)
	}
	if web1-api1 != 5173-8080 {
		t.Errorf("api/web offset mismatch: %d vs %d", api1, web1)
	}
}

func TestPortsForInvalidSlot(t *testing.T) {
	for _, id := range []string{"Agent 1", "../etc", "-x", "slot_name", "toolongslotname0123456789012345678901234567890"} {
		if _, _, err := portsFor(id, false); err == nil {
			t.Errorf("portsFor(%q) expected error, got nil", id)
		}
	}
	// single char is valid per the pattern
	if _, _, err := portsFor("a", false); err != nil {
		t.Errorf("portsFor(\"a\") unexpected error: %v", err)
	}
}

func TestPortsForPathMode(t *testing.T) {
	api1, web1, err := portsFor("./data", true)
	if err != nil {
		t.Fatal(err)
	}
	api2, web2, err := portsFor("data", true)
	if err != nil {
		t.Fatal(err)
	}
	// relative paths resolve against cwd, so "./data" and "data" are the same dir
	if api1 != api2 || web1 != web2 {
		t.Errorf("same dir gave different ports: %d/%d vs %d/%d", api1, web1, api2, web2)
	}
	if api1 < 8081 || api1 > 8179 || web1 < 5174 || web1 > 5272 {
		t.Errorf("ports out of range: %d/%d", api1, web1)
	}
	// different dirs should (almost certainly) differ
	api3, _, err := portsFor("/tmp/other", true)
	if err != nil {
		t.Fatal(err)
	}
	if api3 == api1 {
		t.Logf("note: /tmp/other collides with ./data on api port %d", api3)
	}
}
