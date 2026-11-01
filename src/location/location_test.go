package location

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		value string
		ok    bool
		lat   float64
		lon   float64
	}{
		{"51.5074,-0.1278", true, 51.5074, -0.1278},
		{"51.5074 -0.1278", true, 51.5074, -0.1278},
		{" 51.5 , -0.1 ", true, 51.5, -0.1},
		{"London", false, 0, 0},
		{"91,0", false, 0, 0},
		{"0,181", false, 0, 0},
		{"1", false, 0, 0},
	}
	for _, tc := range cases {
		got, ok := Parse(tc.value)
		if ok != tc.ok {
			t.Fatalf("Parse(%q) ok = %v, want %v", tc.value, ok, tc.ok)
		}
		if ok && (got.Latitude != tc.lat || got.Longitude != tc.lon) {
			t.Fatalf("Parse(%q) = %+v", tc.value, got)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := (Coordinates{Latitude: 51, Longitude: -0.1}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Coordinates{Latitude: 100, Longitude: 0}).Validate(); err == nil {
		t.Fatal("expected an error")
	}
}
