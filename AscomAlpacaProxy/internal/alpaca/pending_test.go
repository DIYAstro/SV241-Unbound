package alpaca

import "testing"

func TestIsPendingOff(t *testing.T) {
	// Shape as decoded from {"status":{"d1":1,"d2":1,"pd":{"d2":[0,873],"d3":[1,20]}}} by the proxy's
	// status cache: JSON numbers arrive as float64, arrays as []interface{}.
	data := map[string]interface{}{
		"d1": float64(1),
		"d2": float64(1),
		"d3": float64(0),
		"pd": map[string]interface{}{
			"d2": []interface{}{float64(0), float64(873)},
			"d3": []interface{}{float64(1), float64(20)},
		},
	}

	cases := []struct {
		name string
		data map[string]interface{}
		key  string
		want bool
	}{
		{"pending off", data, "d2", true},
		{"pending on is not a pending off", data, "d3", false},
		{"nothing pending for this switch", data, "d1", false},
		{"no pd key at all", map[string]interface{}{"d1": float64(1)}, "d1", false},
		{"nil data", nil, "d1", false},
		{"pd of unexpected type", map[string]interface{}{"pd": "oops"}, "d1", false},
		{"entry of unexpected type", map[string]interface{}{"pd": map[string]interface{}{"d1": "oops"}}, "d1", false},
		{"empty entry", map[string]interface{}{"pd": map[string]interface{}{"d1": []interface{}{}}}, "d1", false},
		{"direction of unexpected type", map[string]interface{}{"pd": map[string]interface{}{"d1": []interface{}{"0", float64(5)}}}, "d1", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPendingOff(c.data, c.key); got != c.want {
				t.Errorf("isPendingOff(%q) = %v, want %v", c.key, got, c.want)
			}
		})
	}
}
