package geocode

import (
	"math"
	"testing"
)

func FuzzLocationParsing(f *testing.F) {
	f.Add("51.5074,-0.1278")
	f.Add("SW1A 1AA")
	f.Add("The Venue, 42 Sample Road, London SW1A 1AA")
	f.Add("")
	f.Add("91,0")
	f.Add("NaN,0")
	f.Add(",,")

	f.Fuzz(func(t *testing.T, input string) {
		_ = ExtractUKPostcode(input)
		_ = StripVenueName(input)
		_ = candidateAddresses(input)
		if coord, ok := ParseCoordinates(input); ok {
			if math.IsNaN(coord.Lat) || math.IsNaN(coord.Lon) || math.IsInf(coord.Lat, 0) || math.IsInf(coord.Lon, 0) {
				t.Fatalf("non-finite coordinate accepted for %q: %+v", input, coord)
			}
			if coord.Lat < -90 || coord.Lat > 90 || coord.Lon < -180 || coord.Lon > 180 {
				t.Fatalf("out-of-range coordinate accepted for %q: %+v", input, coord)
			}
		}
	})
}
