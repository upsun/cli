package tests

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
	"github.com/upsun/cli/pkg/mockssh"
)

// fakeDBClients are remote scripts which print their name and arguments, and any input.
var fakeDBClients = map[string]string{
	"mariadb":      `echo "mariadb $*"; cat`,
	"mariadb-dump": `echo "mariadb-dump $*"`,
	"psql":         `echo "psql $*"; cat`,
	"pg_dump":      `echo "pg_dump $*"`,
}

func dbRelationships() map[string]any {
	return map[string]any{
		"database": []any{map[string]any{
			"service": "db", "rel": "mysql", "scheme": "mysql", "type": "mariadb:11.4",
			"host": "database.internal", "port": 3306, "username": "user", "password": "", "path": "main",
		}},
		"pg": []any{map[string]any{
			"service": "pg", "rel": "postgresql", "scheme": "pgsql", "type": "postgresql:16",
			"host": "pg.internal", "port": 5432, "username": "main", "password": "secret", "path": "main",
		}},
	}
}

// setUpDBProject serves an app with database relationships over SSH, with fake database clients.
func setUpDBProject(t *testing.T, clients map[string]string) *testProject {
	t.Helper()
	p := setUpProject(t, sshEnv(&mockapi.Deployment{
		WebApps: map[string]mockapi.App{
			"app": {Name: "app", Type: "golang:1.23", Size: "M", Mounts: map[string]mockapi.Mount{}},
		},
		Services: map[string]mockapi.Service{
			"db": {Type: "mariadb:11.4", Size: "M", Disk: 1024},
			"pg": {Type: "postgresql:16", Size: "M", Disk: 2048},
		},
	}))
	p.serveSSH(t, mockssh.ExecHandler(t.TempDir(), remoteEnv(t, clients, dbRelationships())))
	return p
}

func TestDbDump(t *testing.T) {
	t.Parallel()
	p := setUpDBProject(t, fakeDBClients)
	f := p.f
	args := []string{"-p", layoutProjectID, "-e", "main"}
	mariadbArgs := "--user=user --password= --host=database.internal --port=3306 main"

	_, stdErr, err := f.RunCombinedOutput(append([]string{"db:dump", "-r", "database"}, args...)...)
	require.NoError(t, err)
	dumpFile := filepath.Join(f.dir, layoutProjectID+"--main-xyz--db--main--dump.sql")
	assert.Contains(t, stdErr, "Creating SQL dump file: "+dumpFile)
	assert.Equal(t, "mariadb-dump --single-transaction "+mariadbArgs+"\n", readFile(t, dumpFile))

	// An existing file is overwritten in non-interactive mode.
	_, stdErr, err = f.RunCombinedOutput(append([]string{"db:dump", "-r", "database"}, args...)...)
	require.NoError(t, err)
	assert.Contains(t, stdErr, "File exists: "+dumpFile+". Overwrite? [Y/n] y")

	_, stdErr, err = f.RunCombinedOutput(append([]string{"db:dump", "-r", "database", "--gzip", "-f", "dump.sql.gz",
		"--table", "users", "--table", "posts", "--exclude-table", "cache"}, args...)...)
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Creating gzipped SQL dump file: "+filepath.Join(f.dir, "dump.sql.gz"))
	assert.Equal(t, "mariadb-dump --single-transaction "+mariadbArgs+
		" --ignore-table=main.cache --tables users posts\n", gunzipFile(t, filepath.Join(f.dir, "dump.sql.gz")))

	tableDir := t.TempDir()
	f.Run(append([]string{"db:dump", "-r", "database", "--table", "users", "--schema-only", "-d", tableDir}, args...)...)
	assert.Equal(t, "mariadb-dump --single-transaction "+mariadbArgs+" --no-data --tables users\n",
		readFile(t, filepath.Join(tableDir, layoutProjectID+"--main-xyz--db--main--users--schema--dump.sql")))

	output := f.Run(append([]string{"db:dump", "-r", "pg", "--stdout", "--schema-only",
		"--table", "users", "--exclude-table", "cache", "--charset", "UTF8"}, args...)...)
	assert.Equal(t, "pg_dump --no-owner --if-exists --clean --blobs postgresql://main:secret@pg.internal:5432/main"+
		" --schema-only --table=users --exclude-table=cache --encoding=UTF8\n", output)

	// Another schema.
	output = f.Run(append([]string{"db:dump", "-r", "database", "--stdout", "--schema", "other"}, args...)...)
	assert.Equal(t, "mariadb-dump --single-transaction --user=user --password= --host=database.internal --port=3306 other\n",
		output)
	output = f.Run(append([]string{"db:dump", "-r", "pg", "--stdout", "--schema", "other"}, args...)...)
	assert.Equal(t, "pg_dump --no-owner --if-exists --clean --blobs postgresql://main:secret@pg.internal:5432/other\n", output)

	// A timestamp is inserted before the file extension.
	timestampDir := t.TempDir()
	_, stdErr, err = f.RunCombinedOutput(append([]string{"db:dump", "-r", "database", "--timestamp",
		"-f", filepath.Join(timestampDir, "dump.sql")}, args...)...)
	require.NoError(t, err, stdErr)
	matches, err := filepath.Glob(filepath.Join(timestampDir, "dump--[0-9]*-[0-9]*-*.sql"))
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Contains(t, stdErr, "Creating SQL dump file: "+matches[0])
	_, stdErr, err = f.RunCombinedOutput(append([]string{"db:dump", "-r", "database", "-t", "--schema", "other",
		"-d", timestampDir}, args...)...)
	require.NoError(t, err, stdErr)
	matches, err = filepath.Glob(filepath.Join(timestampDir, layoutProjectID+"--main-xyz--db--other--dump--*.sql"))
	require.NoError(t, err)
	assert.Len(t, matches, 1)

	// The remote exit code is returned.
	failing := setUpDBProject(t, map[string]string{"mariadb-dump": "echo 'dump failed' >&2; exit 3"})
	_, stdErr, err = failing.f.RunCombinedOutput(append([]string{"db:dump", "-r", "database", "--stdout"}, args...)...)
	assertExitCode(t, 3, err)
	assert.Contains(t, stdErr, "dump failed")
}

func TestDbSQL(t *testing.T) {
	t.Parallel()
	p := setUpDBProject(t, fakeDBClients)
	f := p.f
	args := []string{"-p", layoutProjectID, "-e", "main"}

	assert.Equal(t, "mariadb --no-auto-rehash --user=user --password= --host=database.internal --port=3306 main"+
		" --execute SELECT 1\n", f.Run(append([]string{"db:sql", "-r", "database", "SELECT 1"}, args...)...))

	assert.Equal(t, "mariadb --no-auto-rehash --user=user --password= --host=database.internal --port=3306 other"+
		" --batch --raw --execute SELECT 2\n",
		f.Run(append([]string{"db:sql", "-r", "database", "--schema", "other", "--raw", "SELECT 2"}, args...)...))

	assert.Equal(t, "psql postgresql://main:secret@pg.internal:5432/main -t -c SELECT 3\n",
		f.Run(append([]string{"sql", "-r", "pg", "--raw", "SELECT 3"}, args...)...))

	// A query can be piped in.
	f.stdin = strings.NewReader("SELECT 4;\n")
	assert.Equal(t, "psql postgresql://main:secret@pg.internal:5432/main\nSELECT 4;\n",
		f.Run(append([]string{"db:sql", "-r", "pg"}, args...)...))
}

func TestDbSize(t *testing.T) {
	t.Parallel()
	// Each query returns 50 MiB.
	p := setUpDBProject(t, map[string]string{
		"mariadb": "cat > /dev/null; echo 52428800",
		"psql":    "cat > /dev/null; echo 52428800",
	})
	f := p.f
	args := []string{"-p", layoutProjectID, "-e", "main"}

	output, stdErr, err := f.RunCombinedOutput(append([]string{"db:size", "-r", "pg", "--format", "csv"}, args...)...)
	require.NoError(t, err)
	assert.Contains(t, stdErr, "Checking database service pg...")
	assert.Equal(t, "Allocated disk,Estimated usage,% used\n2147483648,52428800,2\n", output)

	output = f.Run(append([]string{"db:size", "-r", "pg", "--no-header"}, args...)...)
	assert.Contains(t, output, "| 2.0 GiB | 50.0 MiB | ~ 2% |")

	// The MySQL usage includes InnoDB and other tables separately.
	output = f.Run(append([]string{"db:size", "-r", "database", "--format", "csv", "--no-header"}, args...)...)
	assert.Equal(t, "1073741824,104857600,10\n", output)
}

func gunzipFile(t *testing.T, filename string) string {
	t.Helper()
	b, err := os.ReadFile(filename)
	require.NoError(t, err)
	r, err := gzip.NewReader(bytes.NewReader(b))
	require.NoError(t, err)
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}
