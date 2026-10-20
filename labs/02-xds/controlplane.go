package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	router "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	server "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
)

const nodeID = "envoy-book"

// One immutable snapshot belongs to one Node.ID group. The mutex serializes
// management writes; SnapshotCache separately synchronizes xDS readers.
type controlPlane struct {
	cache   cache.SnapshotCache
	ctx     context.Context
	mu      sync.Mutex
	version uint64
}

func socketAddress(host string, port uint32) *core.Address {
	return &core.Address{Address: &core.Address_SocketAddress{SocketAddress: &core.SocketAddress{
		Address:       host,
		PortSpecifier: &core.SocketAddress_PortValue{PortValue: port},
	}}}
}

func adsSource() *core.ConfigSource {
	return &core.ConfigSource{
		ResourceApiVersion:    core.ApiVersion_V3,
		ConfigSourceSpecifier: &core.ConfigSource_Ads{Ads: &core.AggregatedConfigSource{}},
	}
}

// makeSnapshot builds fresh protobufs. Never mutate them after publishing.
func makeSnapshot(version, backend string) (*cache.Snapshot, error) {
	if backend != "blue" && backend != "green" {
		return nil, fmt.Errorf("backend must be blue or green")
	}
	resources := map[resource.Type][]types.Resource{}
	for index, color := range []string{"blue", "green"} {
		name := color + "_backend"
		resources[resource.ClusterType] = append(resources[resource.ClusterType], &cluster.Cluster{
			Name:                 name,
			ConnectTimeout:       durationpb.New(time.Second),
			ClusterDiscoveryType: &cluster.Cluster_Type{Type: cluster.Cluster_EDS},
			EdsClusterConfig:     &cluster.Cluster_EdsClusterConfig{EdsConfig: adsSource(), ServiceName: name},
		})
		resources[resource.EndpointType] = append(resources[resource.EndpointType], &endpoint.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints: []*endpoint.LocalityLbEndpoints{{LbEndpoints: []*endpoint.LbEndpoint{{
				HostIdentifier: &endpoint.LbEndpoint_Endpoint{Endpoint: &endpoint.Endpoint{
					Address: socketAddress("127.0.0.1", uint32(8081+index)),
				}},
			}}}},
		})
	}
	resources[resource.RouteType] = []types.Resource{&route.RouteConfiguration{
		Name: "book_routes",
		VirtualHosts: []*route.VirtualHost{{Name: "all_hosts", Domains: []string{"*"}, Routes: []*route.Route{{
			Match: &route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
			Action: &route.Route_Route{Route: &route.RouteAction{
				ClusterSpecifier: &route.RouteAction_Cluster{Cluster: backend + "_backend"},
			}},
		}}}},
	}}
	routerConfig, err := anypb.New(&router.Router{})
	if err != nil {
		return nil, err
	}
	managerConfig, err := anypb.New(&hcm.HttpConnectionManager{
		StatPrefix: "ingress_http",
		RouteSpecifier: &hcm.HttpConnectionManager_Rds{Rds: &hcm.Rds{
			RouteConfigName: "book_routes", ConfigSource: adsSource(),
		}},
		HttpFilters: []*hcm.HttpFilter{{Name: "envoy.filters.http.router", ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: routerConfig}}},
	})
	if err != nil {
		return nil, err
	}
	resources[resource.ListenerType] = []types.Resource{&listener.Listener{
		Name: "listener_0", Address: socketAddress("0.0.0.0", 8080),
		FilterChains: []*listener.FilterChain{{Filters: []*listener.Filter{{
			Name:       "envoy.filters.network.http_connection_manager",
			ConfigType: &listener.Filter_TypedConfig{TypedConfig: managerConfig},
		}}}},
	}}
	snapshot, err := cache.NewSnapshot(version, resources)
	if err != nil {
		return nil, err
	}
	// NewSnapshot does not call Consistent. Check before touching the cache.
	if err := snapshot.Consistent(); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func newControlPlane(ctx context.Context) (*controlPlane, error) {
	cp := &controlPlane{ctx: ctx, cache: cache.NewSnapshotCache(true, cache.IDHash{}, nil)}
	if _, err := cp.publish("blue"); err != nil {
		return nil, err
	}
	return cp, nil
}

func (cp *controlPlane) publish(backend string) (string, error) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	version := "v" + strconv.FormatUint(cp.version+1, 10)
	snapshot, err := makeSnapshot(version, backend)
	if err != nil {
		return "", err
	}
	// SetSnapshot writes before notifying watches. Reserve this version even if
	// notification fails; reusing it could hide a change from connected clients.
	cp.version++
	if err := cp.cache.SetSnapshot(cp.ctx, nodeID, snapshot); err != nil {
		return "", fmt.Errorf("cache may already contain %s: %w", version, err)
	}
	return version, nil
}

func (cp *controlPlane) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("POST /switch", func(w http.ResponseWriter, r *http.Request) {
		backend := r.URL.Query().Get("backend")
		if backend != "blue" && backend != "green" {
			http.Error(w, "backend must be blue or green", http.StatusBadRequest)
			return
		}
		version, err := cp.publish(backend)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Backend string `json:"backend"`
			Version string `json:"version"`
		}{Backend: backend, Version: version})
	})
	return mux
}

func (cp *controlPlane) registerADS(ctx context.Context, grpcServer *grpc.Server, callbacks server.CallbackFuncs) {
	xdsServer := server.NewServer(ctx, cp.cache, callbacks)
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, xdsServer)
}

func backendHandler(color string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, color)
	})
}
