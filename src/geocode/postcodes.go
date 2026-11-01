package geocode

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

var ukPostcode = regexp.MustCompile(`(?i)\b[A-Z]{1,2}[0-9][A-Z0-9]?\s*[0-9][A-Z]{2}\b`)

// ExtractUKPostcode returns the first UK postcode found, normalised.
func ExtractUKPostcode(location string) string {
	match := ukPostcode.FindString(location)
	if match == "" {
		return ""
	}
	return strings.ToUpper(strings.Join(strings.Fields(match), " "))
}

type postcodesIOResponse struct {
	Status int `json:"status"`
	Result *struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"result"`
}

// geocodeViaPostcodesIO resolves a UK postcode using the free postcodes.io API.
func (g *Geocoder) geocodeViaPostcodesIO(ctx context.Context, postcode string) (Coord, bool) {
	normalized := strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(postcode)), " ", "")
	if normalized == "" {
		return Coord{}, false
	}
	endpoint := g.postcodesURL + "/postcodes/" + url.PathEscape(normalized)
	body, err := g.getBody(ctx, endpoint, "postcodes.io request failed")
	if err != nil {
		return Coord{}, false
	}
	var parsed postcodesIOResponse
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Result == nil {
		return Coord{}, false
	}
	lat, lon := parsed.Result.Latitude, parsed.Result.Longitude
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return Coord{}, false
	}
	return Coord{Lat: lat, Lon: lon}, true
}
