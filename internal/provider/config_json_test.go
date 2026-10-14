package provider

import (
	"encoding/json"
	"testing"
)

func TestManagedConfigKeepsOnlyTheUsersKeys(t *testing.T) {
	server := json.RawMessage(`{"method":"GET","expectedStatus":"200-299","timeoutMs":10000,"keyword":{"value":"Add to cart","mode":"contains"},"verifyTls":true}`)
	got, err := managedConfig(`{"keyword":{"mode":"contains","value":"Add to cart"},"expectedStatus":"200-299"}`, server)
	if err != nil {
		t.Fatal(err)
	}
	// Sorted keys, compact, numbers as written: what jsonencode() gives.
	if want := `{"expectedStatus":"200-299","keyword":{"mode":"contains","value":"Add to cart"}}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	// A value changed on the server shows up (drift); the server's defaults don't.
	drifted, _ := managedConfig(`{"timeoutMs":5000}`, server)
	if drifted != `{"timeoutMs":10000}` {
		t.Fatalf("drift not reported: %s", drifted)
	}
	if empty, _ := managedConfig("", server); empty != "" {
		t.Fatalf("no config should stay empty, got %s", empty)
	}
	if _, err := managedConfig(`["not","an","object"]`, server); err == nil {
		t.Fatal("arrays must be refused")
	}
}
