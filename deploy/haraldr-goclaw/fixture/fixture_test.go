package fixture

import "testing"

func TestBrokeredFixture(t *testing.T) {
	if got := "brokered"; got != "brokered" {
		t.Fatalf("bounded fixture mismatch: %q", got)
	}
}
