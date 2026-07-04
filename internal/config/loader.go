package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFile reads slipway.yml.
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return LoadBytes(data)
}

func LoadBytes(data []byte) (Config, error) {
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return ApplyDefaults(cfg), nil
		}
		return Config{}, fmt.Errorf("decode config YAML: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("decode config YAML: %w", err)
	} else if err == nil {
		return Config{}, fmt.Errorf("decode config YAML: multiple documents are not supported")
	}
	return ApplyDefaults(cfg), nil
}
