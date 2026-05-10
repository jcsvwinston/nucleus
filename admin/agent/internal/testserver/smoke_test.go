package testserver

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"testing"
	"time"

	"golang.org/x/net/http2"

	adminv1 "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1"
	adminv1connect "github.com/jcsvwinston/nucleus/admin/proto/gen/go/nucleus/admin/v1/adminv1connect"
)

// TestSmoke_DirectConnect verifies the testserver actually accepts a
// Connect-RPC bidi stream from a manually built h2c client. If this
// passes but the agent integration test fails, the bug is in the agent
// layer, not in the test harness.
func TestSmoke_DirectConnect(t *testing.T) {
	srv := Start()
	defer srv.Close()

	httpClient := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}

	client := adminv1connect.NewAgentServiceClient(httpClient, srv.URL())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream := client.Stream(ctx)
	defer func() {
		_ = stream.CloseRequest()
		_ = stream.CloseResponse()
	}()

	if err := stream.Send(&adminv1.Frame{
		Body: &adminv1.Frame_Registration{
			Registration: &adminv1.NodeRegistration{NodeId: "smoke"},
		},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	reg, err := srv.WaitForRegistration(2 * time.Second)
	if err != nil {
		t.Fatalf("WaitForRegistration: %v", err)
	}
	if reg.NodeId != "smoke" {
		t.Errorf("got %q, want smoke", reg.NodeId)
	}
}
