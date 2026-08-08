package registry

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// ChannelStable is the current stable NixOS channel, used for composable images.
const ChannelStable = "25.11"

// registry.json is generated from https://meta.upsun.com/images by gen.go.
//
//go:generate go run gen.go
//go:embed registry.json
var Data []byte

var parsedRegistry Registry
var parsedOnce sync.Once

func Parse(b []byte) (reg Registry, err error) {
	err = json.Unmarshal(b, &reg)
	if err == nil {
		clean(reg)
	}
	return
}

func Parsed() (Registry, error) {
	var err error
	parsedOnce.Do(func() {
		parsedRegistry, err = Parse(Data)
	})
	return parsedRegistry, err
}

// clean reduces irrelevant information in a registry, for the purposes of this project.
func clean(reg Registry) {
	for k := range reg {
		img := reg[k]
		// Remove deprecated version info.
		img.Versions.Deprecated = nil
		// Remove descriptions.
		img.Description = ""
		reg[k] = img
	}

	// Lisp no longer has its own runtime image.
	// TODO remove this when it's removed upstream
	delete(reg, "lisp")

	// Add missing images.
	if _, ok := reg["clickhouse"]; !ok {
		reg["clickhouse"] = Image{
			Name:     "ClickHouse",
			Type:     "clickhouse",
			Versions: VersionInfo{Supported: []string{"25.3", "24.3", "23.8"}},
		}
	}
	if _, ok := reg["gotenberg"]; !ok {
		reg["gotenberg"] = Image{
			Name:     "Gotenberg",
			Type:     "gotenberg",
			Versions: VersionInfo{Supported: []string{"8"}},
		}
	}
	if _, ok := reg["composable"]; !ok {
		reg["composable"] = Image{
			Name:      "Composable image",
			Type:      "composable",
			Versions:  VersionInfo{Supported: []string{ChannelStable}},
			IsRuntime: true,
		}
	}
	if _, ok := reg["redis-persistent"]; !ok {
		// Treat "redis-persistent" as a copy of "redis".
		if redis, ok := reg["redis"]; ok {
			reg["redis-persistent"] = Image{
				Name:     "Redis (persistent)",
				Type:     "redis-persistent",
				Versions: redis.Versions,
			}
			redis.Name = "Redis (ephemeral)"
			reg["redis"] = redis
		}
	}
	if _, ok := reg["valkey"]; !ok {
		reg["valkey"] = Image{
			Name:     "Valkey",
			Type:     "valkey",
			Versions: VersionInfo{Supported: []string{"8.0"}},
		}
	}
	if _, ok := reg["valkey-persistent"]; !ok {
		// Treat "valkey-persistent" as a copy of "valkey".
		if valkey, ok := reg["valkey"]; ok {
			reg["valkey-persistent"] = Image{
				Name:     "Valkey (persistent)",
				Type:     "valkey-persistent",
				Versions: valkey.Versions,
			}
			valkey.Name = "Valkey (ephemeral)"
			reg["valkey"] = valkey
		}
	}

	// Replica images are deployable but are not listed upstream, or are listed
	// without versions of their own. They track the type they replicate.
	for _, base := range []string{"mariadb", "postgresql"} {
		img, ok := reg[base]
		if !ok {
			continue
		}
		name := base + "-replica"
		replica := reg[name]
		if len(replica.Versions.Supported) > 0 {
			continue
		}
		replica.Name = img.Name + " (replica)"
		replica.Type = name
		replica.Versions = img.Versions
		reg[name] = replica
	}

	// Treat "mysql" as an alias of "mariadb".
	reg["mysql"] = reg["mariadb"]
}
