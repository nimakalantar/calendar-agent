package calendaragent

// Ownership identifies the events a particular agent owns. Calendar adapters
// translate it into their provider's marker (Google private extended property,
// description line, tag, and so on).
type Ownership struct {
	Property string
	Value    string
}

// Filter returns the Google-style private-property filter expression.
func (o Ownership) Filter() string {
	if o.Property == "" {
		return ""
	}
	return o.Property + "=" + o.Value
}

// Matches reports whether a provider property map carries this ownership
// marker.
func (o Ownership) Matches(properties map[string]string) bool {
	if o.Property == "" {
		return false
	}
	return properties[o.Property] == o.Value
}
