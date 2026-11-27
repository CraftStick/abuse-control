package geoip

import "testing"

func TestCityResolverNilAndMissing(t *testing.T) {
	var c *CityResolver
	if _, ok := c.CityInCountry("1.1.1.1", "RU"); ok {
		t.Fatal("nil resolver must not resolve")
	}
	if _, err := NewCityResolver(""); err == nil {
		t.Fatal("empty path must fail")
	}
	if _, err := NewCityResolver("/nonexistent/GeoLite2-City.mmdb"); err == nil {
		t.Fatal("missing file must fail")
	}
}
