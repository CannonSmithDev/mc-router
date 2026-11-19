//go:build wakehold_evidence

package server

import (
	"context"
	"testing"
	"time"

	"github.com/itzg/mc-router/mcproto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Reproducer for PR #669; expected failures document behavior for discussion.
func TestWakeHoldEvidenceAliases(t *testing.T) {
	tests := []struct {
		name string
		host string
	}{
		{
			name: "same hostname control",
			host: "mc.example.com",
		},
		{
			name: "different hostname same target",
			host: "alias.example.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := setupWakeHoldTest(t, time.Minute, wakeHoldTestInterval)
			routes := f.connector.routes
			_, _, target, waker, sleeper, _ := routes.FindBackendForServerAddress(t.Context(), "mc.example.com")
			routes.CreateMapping("alias.example.com", "", target, RouteOptions{Waker: waker, Sleeper: sleeper})

			// Hold the first login before another alias joins the same in-flight wake.
			first := startWakeHoldClient(t, f.ln, mcproto.ProtocolVersion1_18_2, 0)
			first.nextRequest(t)
			second := startWakeHoldClientForHost(t, f.ln, tt.host, mcproto.ProtocolVersion1_18_2, 0)
			select {
			case <-second.requests:
			case ended := <-second.ended:
				require.NotEmpty(t, ended, "client must receive a disconnect rather than just EOF")
				require.Equal(t, byte(mcproto.PacketIdLoginDisconnect), ended[0])
				require.Contains(t, string(ended), wakeKickMessage)
				t.Fatal("second login was kicked instead of joining the shared wake")
			case <-time.After(time.Second):
				t.Fatal("second login did not receive a hold request")
			}

			close(f.backend.release)
			received := [][]byte{f.backend.nextReceived(t), f.backend.nextReceived(t)}
			assert.ElementsMatch(t, [][]byte{first.sent, second.sent}, received)
			assert.Equal(t, backendGreeting, first.disconnectReason(t))
			assert.Equal(t, backendGreeting, second.disconnectReason(t))
			assert.Equal(t, int32(1), f.backend.wakerCalls.Load())
		})
	}
}

func TestWakeHoldEvidenceHandoff(t *testing.T) {
	tests := []struct {
		name      string
		idleDelay time.Duration
	}{
		{
			name:      "2s global delay control",
			idleDelay: 2 * time.Second,
		},
		{
			name:      "300ms global delay",
			idleDelay: 300 * time.Millisecond,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A response remains outstanding for 500ms after the first hold request.
			f := setupWakeHoldTestWithIdleDelay(t, time.Minute, time.Minute, tt.idleDelay)
			routes := f.connector.routes
			_, _, target, waker, sleeper, _ := routes.FindBackendForServerAddress(t.Context(), "mc.example.com")
			// Capture state at the sleep request, independently of the backend's delayed report.
			var client *wakeHoldClient
			type sleepState struct {
				respondedAt int64
				active      int
			}
			sleepRequested := make(chan sleepState, 1)
			observeSleep := func(ctx context.Context) error {
				sleepRequested <- sleepState{
					respondedAt: client.respondedAt.Load(),
					active:      f.connector.scaleActiveConnections.GetCount(target.ScalingKey()),
				}
				return sleeper(ctx)
			}
			routes.UpdateMapping("mc.example.com", "", target, RouteOptions{
				Waker:   waker,
				Sleeper: observeSleep,
			})
			client = startWakeHoldClient(t, f.ln, mcproto.ProtocolVersion1_18_2, 500*time.Millisecond)
			client.nextRequest(t)
			close(f.backend.release)

			// Shutdown must not run while the held player is still being handed off.
			select {
			case <-f.scaledDown:
				state := <-sleepRequested
				t.Fatalf("sleep requested during handoff: response timestamp=%d, admitted connections=%d", state.respondedAt, state.active)
			case received := <-f.backend.received:
				require.Equal(t, client.sent, received)
			case <-time.After(3 * time.Second):
				t.Fatal("held login did not reach the backend")
			}
			assert.Equal(t, backendGreeting, client.disconnectReason(t))
		})
	}
}
