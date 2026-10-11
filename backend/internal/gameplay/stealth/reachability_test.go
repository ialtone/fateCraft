package stealth

import "testing"

func TestFiniteToolsAndIntermediateIntel(t *testing.T) {
	c := Config{Start: "entry", Target: "vault", Exits: []string{"entry"}, Lockpicks: 1,
		Locations: []Location{
			{Key: "entry", Links: []string{"gate"}},
			{Key: "gate", Locked: true, Links: []string{"entry", "vault"}},
			{Key: "vault", Locked: true, Links: []string{"gate"}},
		}}
	if err := validateGateRoute(c); err == nil {
		t.Fatal("one pick opened two gates")
	}
	c.Lockpicks = 2
	if err := validateGateRoute(c); err != nil {
		t.Fatalf("return through unlocked gates should be free: %v", err)
	}
	c.Lockpicks = 1
	c.Locations[1].Intel = true
	if err := validateGateRoute(c); err != nil {
		t.Fatalf("intelligence obtained after one lock should unlock remaining route: %v", err)
	}
	c.Locations[1].Intel = false
	c.Locations[2].Intel = true
	if err := validateGateRoute(c); err == nil {
		t.Fatal("intelligence behind an inaccessible gate unlocked itself")
	}
}
