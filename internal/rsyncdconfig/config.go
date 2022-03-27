package rsyncdconfig

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/gokrazy/rsync/rsyncd"
)

type Listener struct {
	Rsyncd         string `toml:"rsyncd"`
	HTTPMonitoring string `toml:"http_monitoring"`
	AnonSSH        string `toml:"anon_ssh"`
}

type Config struct {
	Listeners []Listener      `toml:"listener"`
	Modules   []rsyncd.Module `toml:"module"`
}

func FromString(input string) (*Config, error) {
	var cfg Config
	if _, err := toml.Decode(input, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func FromFile(path string) (*Config, error) {
	input, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return FromString(string(input))
}

func FromDefaultFiles() (*Config, string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, "", err
	}
	fn := filepath.Join(configDir, "gokr-rsyncd.toml")
	cfg, err := FromFile(fn)
	if err != nil {
		return nil, "", err
	}
	return cfg, fn, nil
}
