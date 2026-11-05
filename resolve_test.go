package pkgregistry

import "testing"

func TestNameMatchPyPI(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"requests", "requests", true},
		{"requests", "Requests", true},
		{"requests", "REQUESTS", true},
		{"requests_lib", "requests-lib", true},
		{"requests.lib", "requests-lib", true},
		{"requests_lib", "requests.lib", true},
		{"requests", "requests2", false},
		{"requests", "requests-lib", false},
	}
	for _, tc := range cases {
		if got := nameMatch(EcosystemPyPI, tc.a, tc.b); got != tc.want {
			t.Errorf("nameMatch(pypi, %q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNameMatchNonPyPI(t *testing.T) {
	cases := []struct {
		ec   string
		a, b string
		want bool
	}{
		{EcosystemNPM, "express", "express", true},
		{EcosystemNPM, "express", "Express", true},
		{EcosystemNPM, "express", "fastify", false},
		{EcosystemCargo, "serde", "Serde", true},
		{EcosystemCargo, "serde", "serde_json", false},
	}
	for _, tc := range cases {
		if got := nameMatch(tc.ec, tc.a, tc.b); got != tc.want {
			t.Errorf("nameMatch(%q, %q, %q) = %v, want %v", tc.ec, tc.a, tc.b, got, tc.want)
		}
	}
}
