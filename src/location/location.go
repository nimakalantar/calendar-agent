// Package location provides geographic coordinates shared by calendar agents.
package location

import (
	"fmt"
	"strconv"
	"strings"
)

// Coordinates is a latitude/longitude pair.
type Coordinates struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// Valid reports whether the coordinates are inside the usual ranges.
func (c Coordinates) Valid() bool {
	return c.Latitude >= -90 && c.Latitude <= 90 && c.Longitude >= -180 && c.Longitude <= 180
}

// String renders the coordinates as "lat,lon".
func (c Coordinates) String() string {
	return strconv.FormatFloat(c.Latitude, 'f', -1, 64) + "," + strconv.FormatFloat(c.Longitude, 'f', -1, 64)
}

// Parse reads "lat,lon" or "lat lon". It reports false for anything else.
func Parse(value string) (Coordinates, bool) {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(parts) != 2 {
		return Coordinates{}, false
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return Coordinates{}, false
	}
	lon, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return Coordinates{}, false
	}
	coords := Coordinates{Latitude: lat, Longitude: lon}
	if !coords.Valid() {
		return Coordinates{}, false
	}
	return coords, true
}

// Validate returns an error for out-of-range coordinates.
func (c Coordinates) Validate() error {
	if !c.Valid() {
		return fmt.Errorf("coordinates out of range: %s", c)
	}
	return nil
}
