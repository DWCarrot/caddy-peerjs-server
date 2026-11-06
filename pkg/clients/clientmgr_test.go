package clients

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DWCarrot/caddy-peerjs-server/pkg/protocol"
	"github.com/DWCarrot/caddy-peerjs-server/pkg/utils"
)

func TestCanceledDeliveryReleasesManager(t *testing.T) {
	manager := NewClientManager(1)
	if err := manager.AddClient(NewClient("blocked", "token", nil)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := manager.SendToMultiClientBatchContext(ctx, map[string]utils.Iterable[*protocol.Message]{"blocked": nil})
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("delivery did not cancel")
	}
	cleared := make(chan struct{})
	go func() { manager.Clear(); close(cleared) }()
	select {
	case <-cleared:
	case <-time.After(time.Second):
		t.Fatal("manager lock was not released")
	}
}
