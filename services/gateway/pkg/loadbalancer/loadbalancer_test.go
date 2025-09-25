
package loadbalancer

import (
	"fmt"
	"sync"
	"testing"

	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
)

func TestNewRoundRobin(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "ep1", Healthy: true},
		{Address: "ep2", Healthy: true},
	}

	rr := NewRoundRobin(endpoints)

	if rr == nil {
		t.Fatal("Expected round robin load balancer to be created")
	}

	if len(rr.endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(rr.endpoints))
	}
}

func TestRoundRobin_Next(t *testing.T) {
	tests := createRoundRobinTests()
	runRoundRobinTests(t, tests)
}

func createRoundRobinTests() []struct {
	name      string
	endpoints []*discovery.Endpoint
	calls     int
	want      []string
} {
	return []struct {
		name      string
		endpoints []*discovery.Endpoint
		calls     int
		want      []string
	}{
		{
			name: "All healthy endpoints",
			endpoints: []*discovery.Endpoint{
				{Address: "ep1", Healthy: true},
				{Address: "ep2", Healthy: true},
				{Address: "ep3", Healthy: true},
			},
			calls: 6,
			want:  []string{"ep2", "ep3", "ep1", "ep2", "ep3", "ep1"},
		},
		{
			name: "Some unhealthy endpoints",
			endpoints: []*discovery.Endpoint{
				{Address: "ep1", Healthy: true},
				{Address: "ep2", Healthy: false},
				{Address: "ep3", Healthy: true},
			},
			calls: 4,
			want:  []string{"ep3", "ep3", "ep1", "ep3"},
		},
		{
			name:      "No endpoints",
			endpoints: []*discovery.Endpoint{},
			calls:     2,
			want:      []string{"", ""},
		},
		{
			name: "All unhealthy",
			endpoints: []*discovery.Endpoint{
				{Address: "ep1", Healthy: false},
				{Address: "ep2", Healthy: false},
			},
			calls: 2,
			want:  []string{"", ""},
		},
	}
}

func runRoundRobinTests(t *testing.T, tests []struct {
	name      string
	endpoints []*discovery.Endpoint
	calls     int
	want      []string
}) {
	t.Helper()
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := NewRoundRobin(tt.endpoints)

			var results []string

			for i := 0; i < tt.calls; i++ {
				ep := rr.Next()

				var got string

				if ep != nil {
					got = ep.Address
				}

				results = append(results, got)

				if i < len(tt.want) && got != tt.want[i] {
					t.Errorf("Call %d: expected %s, got %s", i, tt.want[i], got)
				}
			}

			// Log actual sequence for debugging if test failed
			if t.Failed() && tt.name == "Some unhealthy endpoints" {
				t.Logf("Actual sequence: %v", results)
			}
		})
	}
}

func TestRoundRobin_UpdateEndpoints(t *testing.T) {
	initial := []*discovery.Endpoint{
		{Address: "ep1", Healthy: true},
	}

	rr := NewRoundRobin(initial)

	// Verify initial state
	ep := rr.Next()
	if ep == nil || ep.Address != "ep1" {
		t.Error("Expected ep1 from initial endpoints")
	}

	// Update endpoints
	updated := []*discovery.Endpoint{
		{Address: "ep2", Healthy: true},
		{Address: "ep3", Healthy: true},
	}
	rr.UpdateEndpoints(updated)

	// Verify updated endpoints are used
	seen := make(map[string]bool)

	for i := 0; i < 4; i++ {
		ep := rr.Next()
		if ep != nil {
			seen[ep.Address] = true
		}
	}

	if !seen["ep2"] || !seen["ep3"] {
		t.Error("Expected to see both ep2 and ep3 after update")
	}

	if seen["ep1"] {
		t.Error("Should not see ep1 after update")
	}
}

func TestNewLeastConnections(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "ep1", Healthy: true},
		{Address: "ep2", Healthy: true},
	}

	lc := NewLeastConnections(endpoints)

	if lc == nil {
		t.Fatal("Expected least connections load balancer to be created")
	}

	if len(lc.endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(lc.endpoints))
	}

	if len(lc.connections) != 0 {
		t.Error("Expected empty connections map initially")
	}
}

func TestLeastConnections_Next(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "ep1", Healthy: true},
		{Address: "ep2", Healthy: true},
		{Address: "ep3", Healthy: true},
	}

	lc := NewLeastConnections(endpoints)

	// First calls should distribute evenly
	seen := make(map[string]int)

	for i := 0; i < 3; i++ {
		ep := lc.Next()
		if ep != nil {
			seen[ep.Address]++
		}
	}

	// All endpoints should have been selected once
	for _, ep := range endpoints {
		if seen[ep.Address] != 1 {
			t.Errorf("Expected endpoint %s to be selected once, got %d",
				ep.Address, seen[ep.Address])
		}
	}

	// Release connections and verify selection
	lc.ReleaseConnection("ep1")
	lc.ReleaseConnection("ep1")

	// ep1 should be selected next as it has fewer connections
	ep := lc.Next()
	if ep == nil || ep.Address != "ep1" {
		t.Error("Expected ep1 to be selected after releasing connections")
	}
}

func TestLeastConnections_UpdateEndpoints(t *testing.T) {
	initial := []*discovery.Endpoint{
		{Address: "ep1", Healthy: true},
		{Address: "ep2", Healthy: true},
	}

	lc := NewLeastConnections(initial)

	// Create some connections
	lc.Next() // ep1
	lc.Next() // ep2
	lc.Next() // ep1

	// Update endpoints, keeping ep2
	updated := []*discovery.Endpoint{
		{Address: "ep2", Healthy: true},
		{Address: "ep3", Healthy: true},
	}
	lc.UpdateEndpoints(updated)

	// ep2's connection count should be preserved
	if lc.connections["ep2"] != 1 {
		t.Error("Expected ep2 connection count to be preserved")
	}

	// ep1 should be removed
	if _, exists := lc.connections["ep1"]; exists {
		t.Error("Expected ep1 to be removed from connections")
	}
}

func TestNewWeighted(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "ep1", Healthy: true, Weight: testIterations},
		{Address: "ep2", Healthy: true, Weight: httpStatusOK},
	}

	w := NewWeighted(endpoints)

	if w == nil {
		t.Fatal("Expected weighted load balancer to be created")
	}

	if w.totalWeight != 300 {
		t.Errorf("Expected total weight 300, got %d", w.totalWeight)
	}
}

func TestWeighted_Next(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "ep1", Healthy: true, Weight: 10},
		{Address: "ep2", Healthy: true, Weight: 30},
		{Address: "ep3", Healthy: true, Weight: 60},
	}

	w := NewWeighted(endpoints)

	// Count selections over many iterations
	selections := make(map[string]int)
	iterations := 10000

	for i := 0; i < iterations; i++ {
		ep := w.Next()
		if ep != nil {
			selections[ep.Address]++
		}
	}

	// Verify distribution roughly matches weights
	// ep1: ~10%, ep2: ~30%, ep3: ~60%
	ep1Ratio := float64(selections["ep1"]) / float64(iterations)
	ep2Ratio := float64(selections["ep2"]) / float64(iterations)
	ep3Ratio := float64(selections["ep3"]) / float64(iterations)

	if ep1Ratio < 0.08 || ep1Ratio > 0.12 {
		t.Errorf("ep1 selection ratio %f outside expected range [0.08, 0.12]", ep1Ratio)
	}

	if ep2Ratio < 0.28 || ep2Ratio > 0.32 {
		t.Errorf("ep2 selection ratio %f outside expected range [0.28, 0.32]", ep2Ratio)
	}

	if ep3Ratio < 0.58 || ep3Ratio > 0.62 {
		t.Errorf("ep3 selection ratio %f outside expected range [0.58, 0.62]", ep3Ratio)
	}
}

func TestWeighted_ZeroWeight(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "ep1", Healthy: true, Weight: 0},
		{Address: "ep2", Healthy: true, Weight: 0},
	}

	w := NewWeighted(endpoints)

	// Zero weights should be treated as 1
	if w.totalWeight != 2 {
		t.Errorf("Expected total weight 2, got %d", w.totalWeight)
	}

	// Should still select endpoints
	seen := make(map[string]bool)

	for i := 0; i < 10; i++ {
		ep := w.Next()
		if ep != nil {
			seen[ep.Address] = true
		}
	}

	if len(seen) != 2 {
		t.Error("Expected both endpoints to be selected with zero weight")
	}
}

func TestLoadBalancer_ConcurrentAccess(t *testing.T) {
	endpoints := createConcurrentTestEndpoints()
	loadBalancers := createLoadBalancerVariants(endpoints)
	
	runConcurrentAccessTests(t, loadBalancers)
}

func createConcurrentTestEndpoints() []*discovery.Endpoint {
	endpoints := make([]*discovery.Endpoint, 10)
	for i := range endpoints {
		endpoints[i] = &discovery.Endpoint{
			Address: fmt.Sprintf("ep%d", i),
			Healthy: true,
			Weight:  testIterations,
		}
	}
	return endpoints
}

func createLoadBalancerVariants(endpoints []*discovery.Endpoint) []struct {
	name string
	lb   LoadBalancer
} {
	return []struct {
		name string
		lb   LoadBalancer
	}{
		{"RoundRobin", NewRoundRobin(endpoints)},
		{"LeastConnections", NewLeastConnections(endpoints)},
		{"Weighted", NewWeighted(endpoints)},
	}
}

func runConcurrentAccessTests(t *testing.T, loadBalancers []struct {
	name string
	lb   LoadBalancer
}) {
	t.Helper()
	
	for _, tt := range loadBalancers {
		t.Run(tt.name, func(t *testing.T) {
			var wg sync.WaitGroup

			selections := &sync.Map{}

			// Concurrent selections
			for i := 0; i < testIterations; i++ {
				wg.Add(1)

				go func() {
					defer wg.Done()

					for j := 0; j < testIterations; j++ {
						ep := tt.lb.Next()
						if ep != nil {
							selections.Store(ep.Address, true)
						}
					}
				}()
			}

			// Concurrent updates
			for i := 0; i < 10; i++ {
				wg.Add(1)

				go func(iter int) {
					defer wg.Done()

					newEndpoints := make([]*discovery.Endpoint, 5)
					for j := range newEndpoints {
						newEndpoints[j] = &discovery.Endpoint{
							Address: fmt.Sprintf("new-ep%d-%d", iter, j),
							Healthy: true,
							Weight:  testIterations,
						}
					}

					tt.lb.UpdateEndpoints(newEndpoints)
				}(i)
			}

			wg.Wait()

			// Verify some endpoints were selected
			count := 0

			selections.Range(func(_, _ interface{}) bool {
				count++
				return true
			})

			if count == 0 {
				t.Error("Expected some endpoints to be selected")
			}
		})
	}
}

func BenchmarkRoundRobin_Next(b *testing.B) {
	endpoints := make([]*discovery.Endpoint, testIterations)
	for i := range endpoints {
		endpoints[i] = &discovery.Endpoint{
			Address: fmt.Sprintf("ep%d", i),
			Healthy: true,
		}
	}

	rr := NewRoundRobin(endpoints)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rr.Next()
	}
}

func BenchmarkLeastConnections_Next(b *testing.B) {
	endpoints := make([]*discovery.Endpoint, testIterations)
	for i := range endpoints {
		endpoints[i] = &discovery.Endpoint{
			Address: fmt.Sprintf("ep%d", i),
			Healthy: true,
		}
	}

	lc := NewLeastConnections(endpoints)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ep := lc.Next()
		if i%10 == 0 && ep != nil {
			lc.ReleaseConnection(ep.Address)
		}
	}
}

func BenchmarkWeighted_Next(b *testing.B) {
	endpoints := make([]*discovery.Endpoint, testIterations)
	for i := range endpoints {
		endpoints[i] = &discovery.Endpoint{
			Address: fmt.Sprintf("ep%d", i),
			Healthy: true,
			Weight:  i + 1,
		}
	}

	w := NewWeighted(endpoints)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		w.Next()
	}
}
