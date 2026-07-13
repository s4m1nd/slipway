package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Config is the root slipway.yml model.
type Config struct {
	Project      string                 `json:"project" yaml:"project"`
	Retention    Retention              `json:"retention" yaml:"retention"`
	Registry     Registry               `json:"registry" yaml:"registry"`
	Defaults     Defaults               `json:"-" yaml:"-"`
	Secrets      Secrets                `json:"secrets" yaml:"secrets"`
	Environments map[string]Environment `json:"environments" yaml:"environments"`
}

type Retention struct {
	Releases    int  `json:"releases" yaml:"releases"`
	releasesSet bool `json:"-" yaml:"-"`
}

func (r *Retention) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("retention must be a mapping")
	}
	for i := 0; i < len(value.Content); i += 2 {
		key := value.Content[i]
		val := value.Content[i+1]
		switch key.Value {
		case "releases":
			var releases int
			if err := val.Decode(&releases); err != nil {
				return fmt.Errorf("retention.releases: %w", err)
			}
			r.Releases = releases
			r.releasesSet = true
		default:
			return fmt.Errorf("field %s not found in type config.Retention", key.Value)
		}
	}
	return nil
}

type Registry struct {
	Server         string   `json:"server" yaml:"server"`
	Username       string   `json:"username" yaml:"username"`
	PasswordSecret string   `json:"password_secret" yaml:"password_secret"`
	Password       []string `json:"password" yaml:"password"`
}

type Defaults struct {
	Root string `json:"-" yaml:"-"`
}

type Secrets struct {
	Fetch    string         `json:"fetch" yaml:"fetch"`
	Provider SecretProvider `json:"provider" yaml:"provider"`
	Names    []string       `json:"names" yaml:"names"`
}

type SecretProvider struct {
	Type        string `json:"type" yaml:"type"`
	Account     string `json:"account" yaml:"account"`
	Vault       string `json:"vault" yaml:"vault"`
	Item        string `json:"item" yaml:"item"`
	FieldPrefix string `json:"field_prefix" yaml:"field_prefix"`
}

type Environment struct {
	Retention   Retention            `json:"retention" yaml:"retention"`
	Servers     map[string]Server    `json:"servers" yaml:"servers"`
	Proxy       Proxy                `json:"proxy" yaml:"proxy"`
	Accessories map[string]Accessory `json:"accessories" yaml:"accessories"`
	Services    map[string]Service   `json:"services" yaml:"services"`
}

type Server struct {
	Host    string `json:"host" yaml:"host"`
	SSHUser string `json:"ssh_user" yaml:"ssh_user"`
	SSHPort int    `json:"host_ssh_port" yaml:"host_ssh_port"`
}

type Proxy struct {
	ListenHTTP  string       `json:"listen_http" yaml:"listen_http"`
	ListenHTTPS string       `json:"listen_https" yaml:"listen_https"`
	Routes      []ProxyRoute `json:"routes" yaml:"routes"`
}

type ProxyRoute struct {
	Host    string `json:"host" yaml:"host"`
	Service string `json:"service" yaml:"service"`
	TLS     bool   `json:"tls" yaml:"tls"`
}

// Accessory is a stable, persistent container managed independently from
// blue/green application releases.
type Accessory struct {
	Type    string            `json:"type" yaml:"type"`
	Image   string            `json:"image" yaml:"image"`
	Host    string            `json:"host" yaml:"host"`
	Env     map[string]string `json:"env" yaml:"env"`
	Secrets []string          `json:"secrets" yaml:"secrets"`
	Storage AccessoryStorage  `json:"storage" yaml:"storage"`
}

type AccessoryStorage struct {
	Volume string `json:"volume" yaml:"volume"`
}

type Service struct {
	Image        string            `json:"image" yaml:"image"`
	Build        Build             `json:"build" yaml:"build"`
	Hosts        []string          `json:"hosts" yaml:"hosts"`
	DependsOn    []string          `json:"depends_on" yaml:"depends_on"`
	InternalPort int               `json:"internal_port" yaml:"internal_port"`
	HealthCheck  HealthCheck       `json:"health_check" yaml:"health_check"`
	Env          map[string]string `json:"env" yaml:"env"`
	Secrets      []string          `json:"secrets" yaml:"secrets"`
}

type Build struct {
	Context    string   `json:"context" yaml:"context"`
	Dockerfile string   `json:"dockerfile" yaml:"dockerfile"`
	Platform   string   `json:"platform" yaml:"platform"`
	Target     string   `json:"target" yaml:"target"`
	Args       []string `json:"args" yaml:"args"`
}

type HealthCheck struct {
	Path     string `json:"path" yaml:"path"`
	Port     int    `json:"port" yaml:"port"`
	Interval string `json:"interval" yaml:"interval"`
	Timeout  string `json:"timeout" yaml:"timeout"`
	Retries  int    `json:"retries" yaml:"retries"`
}
