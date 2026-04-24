package apiserver_test

import (
	"testing"

	"github.com/piotrmiskiewicz/custom-api-server/pkg/apiserver"
)

func TestNew_ReturnsServer(t *testing.T) {
	srv, err := apiserver.New("", "", ":0")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if srv == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNew_HasAPIsRoute(t *testing.T) {
	srv, err := apiserver.New("", "", ":0")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	found := false
	for _, p := range srv.Handler.ListedPaths() {
		if p == "/apis" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected /apis to be listed in handler paths")
	}
}
