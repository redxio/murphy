package conf

import (
	"net"
	"net/url"
	"strconv"
)

type Headless struct {
	Enable  bool `json:"enable"`
	Timeout int  `json:"timeout"`
}

type Proxy struct {
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
	Address  string `json:"address"`
}

type Option struct {
	Depth            int  `json:"depth"`
	MaxConcurrency   int  `json:"maxConcurrency"`
	ExternalWebpages bool `json:"externelWebpages"`
}

type Configuration struct {
	Log           string   `json:"log"`
	Dir           string   `json:"dir"`
	FileTypes     []string `json:"fileTypes"`
	Proxy         *Proxy   `json:"proxy"`
	Option        *Option  `json:"option"`
	Headless      Headless `json:"headless"`
	UserAgent     string   `json:"userAgent"`
	DisableCookie bool     `json:"enableCookie"`
}

func (p *Proxy) URL() *url.URL {
	return &url.URL{
		Scheme: p.Protocol,
		Host:   net.JoinHostPort(p.Address, strconv.FormatInt(int64(p.Port), 10)),
	}
}
