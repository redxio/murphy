package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/NzKSO/murphy/conf"
)

func readConfigFile(path string) (*conf.Configuration, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config conf.Configuration
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if config.Option.Depth < 0 {
		return nil, errors.New("invalid depth")
	}

	if config.Option.MaxConcurrency < 0 {
		return nil, errors.New("invalid max concurrency")
	}
	return &config, err
}

func overrideConfig(config *conf.Configuration) error {
	var xFlag string
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i][0] == '-' {
			if len(os.Args[i]) == 1 || (os.Args[i][1] == '-' && len(os.Args[i]) == 2) {
				break
			}
			if os.Args[i][1] != '-' {
				xFlag = os.Args[i][1:]
			} else {
				xFlag = os.Args[i][2:]
			}
		} else {
			break
		}
		if idx := strings.IndexByte(xFlag, '='); idx > 0 {
			xFlag = xFlag[:idx]
		} else if xFlag != "external" && xFlag != "headless" {
			i++
		}
		switch xFlag {
		case "proxy":
			u, err := url.ParseRequestURI(proxyAddr)
			if err != nil {
				return err
			}
			port, err := strconv.Atoi(u.Port())
			if err != nil {
				return err
			}
			if config.Proxy == nil {
				config.Proxy = new(conf.Proxy)
			}
			config.Proxy.Address = u.Hostname()
			config.Proxy.Port = port
			config.Proxy.Protocol = u.Scheme
		case "depth":
			if depth < -1 {
				return errors.New("Flag depth: " + invalidDepth)
			}
			config.Option.Depth = depth
		case "maxCon":
			if maxCon < 0 {
				return errors.New("Flag maxCon: " + invalidMaxCon)
			}
			config.Option.MaxConcurrency = maxCon
		case "ftypes":
			if len(ftypes) < 1 {
				return errors.New("Flag ftypes: " + invalidftypes)
			}
			config.FileTypes = []string(ftypes)
		case "external":
			config.Option.ExternalWebpages = external
		case "dir":
			config.Dir = prefix
		case "log":
			config.Log = logFile
			// case "headless":
			// 	config.Headless = headlessMode
		}
	}
	if len(config.FileTypes) < 1 {
		return errors.New(emptyListSet)
	}
	return nil
}
