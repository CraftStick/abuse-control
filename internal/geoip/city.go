package geoip

import (
	"fmt"
	"net/netip"
	"sync"

	"github.com/oschwald/maxminddb-golang/v2"
)

type CityResolver struct {
	mu     sync.RWMutex
	reader *maxminddb.Reader
}

func NewCityResolver(path string) (*CityResolver, error) {
	if path == "" {
		return nil, fmt.Errorf("geoip: city database path is empty")
	}
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("geoip: open %s: %w", path, err)
	}
	return &CityResolver{reader: r}, nil
}

func (c *CityResolver) Reload(path string) error {
	if c == nil {
		return fmt.Errorf("geoip: Reload called on nil CityResolver")
	}
	r, err := maxminddb.Open(path)
	if err != nil {
		return fmt.Errorf("geoip: reload open %s: %w", path, err)
	}
	c.mu.Lock()
	old := c.reader
	c.reader = r
	c.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (c *CityResolver) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	old := c.reader
	c.reader = nil
	c.mu.Unlock()
	if old == nil {
		return nil
	}
	return old.Close()
}

func (c *CityResolver) CityInCountry(ipStr, country string) (string, bool) {
	if c == nil {
		return "", false
	}
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return "", false
	}
	var rec struct {
		City struct {
			Names map[string]string `maxminddb:"names"`
		} `maxminddb:"city"`
		Country struct {
			ISO string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.reader == nil {
		return "", false
	}
	if err := c.reader.Lookup(ip.Unmap()).Decode(&rec); err != nil {
		return "", false
	}
	name := rec.City.Names["en"]
	if name == "" || rec.Country.ISO != country {
		return "", false
	}
	return name, true
}
