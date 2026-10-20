package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	server "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestSnapshotReferencesAndValidation(t *testing.T) {
	for _, backend := range []string{"blue", "green"} {
		t.Run(backend, func(t *testing.T) {
			snapshot, err := makeSnapshot("v1", backend)
			if err != nil {
				t.Fatal(err)
			}
			if err := snapshot.Consistent(); err != nil {
				t.Fatal(err)
			}
			for _, typ := range []string{resource.ClusterType, resource.EndpointType, resource.ListenerType, resource.RouteType} {
				if got := snapshot.GetVersion(typ); got != "v1" {
					t.Fatalf("version = %q", got)
				}
				for name, res := range snapshot.GetResources(typ) {
					if err := res.(interface{ ValidateAll() error }).ValidateAll(); err != nil {
						t.Fatalf("%s: %v", name, err)
					}
				}
			}
			clusters := snapshot.GetResources(resource.ClusterType)
			for _, color := range []string{"blue", "green"} {
				name := color + "_backend"
				c := clusters[name].(*cluster.Cluster)
				if c.GetType() != cluster.Cluster_EDS || c.GetEdsClusterConfig().GetEdsConfig().GetAds() == nil {
					t.Fatalf("%s is not ADS EDS", name)
				}
				e := snapshot.GetResources(resource.EndpointType)[name].(*endpoint.ClusterLoadAssignment)
				address := e.GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress()
				if address.GetAddress() != "127.0.0.1" || address.GetPortValue() == 0 {
					t.Fatalf("unexpected endpoint: %v", address)
				}
			}
			rc := snapshot.GetResources(resource.RouteType)["book_routes"].(*route.RouteConfiguration)
			if got := rc.GetVirtualHosts()[0].GetRoutes()[0].GetRoute().GetCluster(); got != backend+"_backend" {
				t.Fatalf("route points to %q", got)
			}
			l := snapshot.GetResources(resource.ListenerType)["listener_0"].(*listener.Listener)
			manager := new(hcm.HttpConnectionManager)
			if err := l.GetFilterChains()[0].GetFilters()[0].GetTypedConfig().UnmarshalTo(manager); err != nil {
				t.Fatal(err)
			}
			if manager.GetRds().GetRouteConfigName() != "book_routes" {
				t.Fatal("LDS does not reference RDS")
			}
		})
	}
}

func TestSnapshotRejectsUnknownBackend(t *testing.T) {
	if _, err := makeSnapshot("v1", "purple"); err == nil {
		t.Fatal("unknown backend accepted")
	}
}

func newTestControlPlane(t *testing.T) *controlPlane {
	t.Helper()
	cp, err := newControlPlane(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

func TestNodeIDSelectsSnapshot(t *testing.T) {
	cp := newTestControlPlane(t)
	group := cache.IDHash{}.ID(&core.Node{Id: "envoy-book", Cluster: "irrelevant"})
	if _, err := cp.cache.GetSnapshot(group); err != nil {
		t.Fatal(err)
	}
	if _, err := cp.cache.GetSnapshot("another-node"); err == nil {
		t.Fatal("unexpected snapshot for another node")
	}
}

func TestSwitchPublishesNewVersion(t *testing.T) {
	cp := newTestControlPlane(t)
	r := httptest.NewRequest(http.MethodPost, "/switch?backend=green", nil)
	w := httptest.NewRecorder()
	cp.handler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var state struct{ Backend, Version string }
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Backend != "green" || state.Version != "v2" {
		t.Fatalf("state=%+v", state)
	}
	snapshot, err := cp.cache.GetSnapshot(nodeID)
	if err != nil {
		t.Fatal(err)
	}
	rc := snapshot.GetResources(resource.RouteType)["book_routes"].(*route.RouteConfiguration)
	if got := rc.GetVirtualHosts()[0].GetRoutes()[0].GetRoute().GetCluster(); got != "green_backend" {
		t.Fatalf("cluster=%q", got)
	}
}

func TestInvalidSwitchDoesNotPublish(t *testing.T) {
	cp := newTestControlPlane(t)
	before, _ := cp.cache.GetSnapshot(nodeID)
	for _, tc := range []struct {
		method, url string
		status      int
	}{
		{http.MethodPost, "/switch?backend=purple", http.StatusBadRequest},
		{http.MethodPost, "/switch", http.StatusBadRequest},
		{http.MethodGet, "/switch?backend=green", http.StatusMethodNotAllowed},
	} {
		w := httptest.NewRecorder()
		cp.handler().ServeHTTP(w, httptest.NewRequest(tc.method, tc.url, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: status=%d", tc.method, tc.url, w.Code)
		}
		after, _ := cp.cache.GetSnapshot(nodeID)
		if before != after {
			t.Fatal("invalid request published a snapshot")
		}
	}
}

func TestADSReceivesSnapshotACKAndUpdate(t *testing.T) {
	cp := newTestControlPlane(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ack := make(chan string, 8)
	callbacks := server.CallbackFuncs{StreamRequestFunc: func(_ int64, req *discovery.DiscoveryRequest) error {
		if req.GetResponseNonce() != "" && req.GetErrorDetail() == nil {
			ack <- req.GetVersionInfo()
		}
		return nil
	}}
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	cp.registerADS(ctx, grpcServer, callbacks)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stream, err := discovery.NewAggregatedDiscoveryServiceClient(conn).StreamAggregatedResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request := &discovery.DiscoveryRequest{Node: &core.Node{Id: nodeID}, TypeUrl: resource.RouteType, ResourceNames: []string{"book_routes"}}
	if err := stream.Send(request); err != nil {
		t.Fatal(err)
	}
	response, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	assertADSRoute(t, response, "v1", "blue_backend")
	request.Node = nil // Envoy may send Node only on the first message.
	request.VersionInfo, request.ResponseNonce = response.GetVersionInfo(), response.GetNonce()
	if err := stream.Send(request); err != nil {
		t.Fatal(err)
	}
	select {
	case version := <-ack:
		if version != "v1" {
			t.Fatalf("ACK=%q", version)
		}
	case <-ctx.Done():
		t.Fatal("no ACK observed")
	}
	w := httptest.NewRecorder()
	cp.handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/switch?backend=green", nil))
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	response, err = stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	assertADSRoute(t, response, "v2", "green_backend")
	request.VersionInfo, request.ResponseNonce = response.GetVersionInfo(), response.GetNonce()
	if err := stream.Send(request); err != nil {
		t.Fatal(err)
	}
	select {
	case version := <-ack:
		if version != "v2" {
			t.Fatalf("ACK=%q", version)
		}
	case <-ctx.Done():
		t.Fatal("no update ACK observed")
	}
}

func assertADSRoute(t *testing.T, response *discovery.DiscoveryResponse, version, target string) {
	t.Helper()
	if response.GetVersionInfo() != version || response.GetNonce() == "" || len(response.GetResources()) != 1 {
		t.Fatalf("response=%v", response)
	}
	rc := new(route.RouteConfiguration)
	if err := response.GetResources()[0].UnmarshalTo(rc); err != nil {
		t.Fatal(err)
	}
	if got := rc.GetVirtualHosts()[0].GetRoutes()[0].GetRoute().GetCluster(); got != target {
		t.Fatalf("target=%q", got)
	}
}

func TestBackendRespondsWithColor(t *testing.T) {
	for _, color := range []string{"blue", "green"} {
		s := httptest.NewServer(backendHandler(color))
		resp, err := http.Get(s.URL + "/anything")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		s.Close()
		if err != nil || strings.TrimSpace(string(body)) != color {
			t.Fatalf("body=%q err=%v", body, err)
		}
	}
}
