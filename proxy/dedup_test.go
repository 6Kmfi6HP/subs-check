package proxy

import (
	"reflect"
	"testing"
)

func TestDeduplicateProxies(t *testing.T) {
	proxy1 := map[string]any{"name": "p1", "server": "1.1.1.1", "port": 1000, "password": "a"}
	proxy2 := map[string]any{"name": "p2", "server": "1.1.1.1", "port": 1000, "password": "a"} // duplicate of p1
	proxy3 := map[string]any{"name": "p3", "server": "2.2.2.2", "port": 1000, "password": "a"}
	proxy4 := map[string]any{"name": "p4", "server": "1.1.1.1", "port": 2000, "password": "a"} // different port
	proxy5 := map[string]any{"name": "p5", "server": "1.1.1.1", "port": 1000, "password": "b"} // different password
	proxy6 := map[string]any{"name": "p6", "server": "1.1.1.1", "port": 1000, "uuid": "a"}     // same as p1, but with uuid
	proxy7 := map[string]any{"name": "p7", "server": "3.3.3.3", "port": 1000, "uuid": "c"}
	proxy8 := map[string]any{"name": "p8", "server": "1.1.1.1", "port": 1000, "password": "a", "servername": "sni.com"} // different servername
	proxy9 := map[string]any{"name": "p9", "server": "1.1.1.1", "port": 1000, "password": "a", "servername": "sni.com"} // duplicate of p8

	testCases := []struct {
		name  string
		input []map[string]any
		want  []map[string]any
	}{
		{
			name:  "No duplicates",
			input: []map[string]any{proxy1, proxy3, proxy4},
			want:  []map[string]any{proxy1, proxy3, proxy4},
		},
		{
			name:  "Simple duplicate",
			input: []map[string]any{proxy1, proxy2, proxy3},
			want:  []map[string]any{proxy1, proxy3},
		},
		{
			name:  "All duplicates",
			input: []map[string]any{proxy1, proxy2, proxy6},
			want:  []map[string]any{proxy1},
		},
		{
			name:  "Empty input",
			input: []map[string]any{},
			want:  []map[string]any{},
		},
		{
			name:  "Complex case with various differences",
			input: []map[string]any{proxy1, proxy2, proxy3, proxy4, proxy5, proxy6, proxy7, proxy8, proxy9},
			want:  []map[string]any{proxy1, proxy3, proxy4, proxy5, proxy7, proxy8},
		},
		{
			name:  "Nil input",
			input: nil,
			want:  []map[string]any{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeduplicateProxies(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DeduplicateProxies() = %v, want %v", got, tc.want)
			}
		})
	}
}
