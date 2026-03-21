package media

import "testing"

func TestNewHandlerBindsService(t *testing.T) {
	svc := &Service{}
	handler := NewHandler(svc)
	if handler == nil {
		t.Fatal("expected handler")
	}
	if handler.Svc != svc {
		t.Fatal("expected handler to keep service reference")
	}
}
