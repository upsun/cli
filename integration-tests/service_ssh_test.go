package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

// setUpServiceProject serves an app with Redis and MongoDB relationships over SSH, with fake clients.
func setUpServiceProject(t *testing.T) *testProject {
	t.Helper()
	p := setUpProject(t, sshEnv(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Mounts: map[string]mockapi.Mount{}},
		},
	}))
	relationships := map[string]any{
		"redis": []any{map[string]any{
			"service": "cache", "scheme": "redis", "type": "redis:7.2", "host": "redis.internal", "port": 6379,
		}},
		"mongo": []any{map[string]any{
			"service": "mongo", "scheme": "mongodb", "type": "mongodb-enterprise:7.0", "host": "mongo.internal",
			"port": 27017, "username": "main", "password": "secret", "path": "main",
		}},
	}
	clients := map[string]string{}
	for _, name := range []string{"redis-cli", "mongo", "mongodump", "mongoexport"} {
		clients[name] = `echo "` + name + ` $*"`
	}
	clients["mongorestore"] = `echo "mongorestore $*"; cat`
	p.serveSSH(t, mockssh.ExecHandler(t.TempDir(), remoteEnv(t, clients, relationships)))
	return p
}

func TestRedisCLI(t *testing.T) {
	t.Parallel()
	f := setUpServiceProject(t).f
	args := []string{"-p", layoutProjectID, "-e", "main"}

	assert.Equal(t, "redis-cli -h redis.internal -p 6379 ping\n",
		f.Run(append([]string{"service:redis-cli", "ping"}, args...)...))
	assert.Equal(t, "redis-cli -h redis.internal -p 6379 --scan --pattern *-11*\n",
		f.Run(append(append([]string{"redis", "-r", "redis"}, args...), "--", "--scan --pattern '*-11*'")...))

	_, stdErr, err := f.RunCombinedOutput(append([]string{"redis", "-r", "mongo", "ping"}, args...)...)
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Relationship not found: mongo")
}

func TestMongo(t *testing.T) {
	t.Parallel()
	f := setUpServiceProject(t).f
	args := []string{"-p", layoutProjectID, "-e", "main"}
	connArgs := "--username main --password secret --host mongo.internal --port 27017 --authenticationDatabase main"

	assert.Equal(t, "mongo "+connArgs+" main --eval db.stats() --quiet\n",
		f.Run(append([]string{"service:mongo:shell", "--eval", "db.stats()"}, args...)...))

	assert.Equal(t, "mongoexport "+connArgs+" --db main --collection users --type csv --fields name,email --quiet\n",
		f.Run(append([]string{"mongoexport", "-c", "users", "--type", "csv", "-f", "name", "-f", "email"}, args...)...))

	_, stdErr, err := f.RunCombinedOutput(append([]string{"service:mongo:dump", "-c", "users", "--gzip"}, args...)...)
	require.NoError(t, err)
	dumpFile := filepath.Join(f.dir, layoutProjectID+"--main--app--users--archive.bson.gz")
	assert.Contains(t, stdErr, "Creating gzipped BSON archive file: "+filepath.Base(dumpFile))
	assert.Equal(t, "mongodump "+connArgs+" --db main --collection users --archive --gzip\n", readFile(t, dumpFile))

	assert.Equal(t, "mongodump "+connArgs+" --db main --archive\n",
		f.Run(append([]string{"mongodump", "--stdout"}, args...)...))

	f.stdin = strings.NewReader("archive data")
	assert.Equal(t, "mongorestore "+connArgs+" --db main --collection users --archive\narchive data",
		f.Run(append([]string{"service:mongo:restore", "-c", "users"}, args...)...))
}
