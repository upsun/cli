package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/upsun/cli/pkg/mockapi"
)

const mib = 1024 * 1024

// fakeMysqlServer is a fake "mariadb" client, which answers the queries of db:mysql-optimize.
type fakeMysqlServer struct {
	vars   map[string]string
	tables [][]any // schema, table, data_length, index_length, data_free, offline_indexes, same_free
}

// script returns the fake client, which logs each query to a file in dir.
func (s fakeMysqlServer) script(t *testing.T, dir string) string {
	var vars, tables strings.Builder
	for k, v := range s.vars {
		fmt.Fprintf(&vars, "%s\t%s\n", k, v)
	}
	for _, row := range s.tables {
		for i, v := range row {
			if i > 0 {
				tables.WriteString("\t")
			}
			fmt.Fprint(&tables, v)
		}
		tables.WriteString("\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vars.tsv"), []byte(vars.String()), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tables.tsv"), []byte(tables.String()), 0o600))
	return fmt.Sprintf(`dir=%q
q="$(cat)"
printf '%%s\n---\n' "$q" >> "$dir/queries.log"
case "$q" in
  *"SHOW GLOBAL VARIABLES"*) cat "$dir/vars.tsv" ;;
  *"ALTER TABLE"*|*"ANALYZE TABLE"*) ;;
  *"FROM information_schema.TABLES t"*)
    # Apply table name filters.
    while IFS=$'\t' read -r schema name rest; do
      case "$q" in *"t.TABLE_NAME = "*)
        case "$q" in *"t.TABLE_NAME = '$name'"*) ;; *) continue ;; esac ;;
      esac
      printf '%%s\t%%s\t%%s\n' "$schema" "$name" "$rest"
    done < "$dir/tables.tsv" ;;
  *) echo "unexpected query: $q" >&2; exit 1 ;;
esac
`, dir)
}

// setUpMysqlOptimizeProject serves an app with database relationships, with a fake MariaDB client, and disk metrics
// for the "db" service. It returns the project and a function returning the queries run so far.
func setUpMysqlOptimizeProject(t *testing.T, server fakeMysqlServer, withMetrics bool) (
	p *testProject, queries func() []string,
) {
	t.Helper()
	dir := t.TempDir()
	logFile := filepath.Join(dir, "queries.log")
	p = setUpDBProject(t, map[string]string{"mariadb": server.script(t, dir)})

	envPath := "/projects/" + layoutProjectID + "/environments/main"
	overviewPath := envPath + "/observability/resources/overview"
	p.api.Get(envPath+"/observability/", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_links": mockapi.MakeHALLinks("resources_overview=" + p.f.apiURL + overviewPath),
		})
	})
	if withMetrics {
		p.api.Get(overviewPath, func(w http.ResponseWriter, req *http.Request) {
			assert.Equal(t, []string{"db"}, req.URL.Query()["services[0]"])
			_ = json.NewEncoder(w).Encode(map[string]any{
				"_grain": 60, "_from": 1790189400, "_to": 1790190000,
				"data": []any{
					map[string]any{"timestamp": 1790189940, "services": map[string]any{"db": map[string]any{
						"mountpoints": map[string]any{
							"/mnt": mountpointMetrics(1024*mib, 4096*mib, 10, 1000),
							"/tmp": mountpointMetrics(100*mib, 1024*mib, 1, 1000),
						},
					}}},
					// The in-progress point has no services.
					map[string]any{"timestamp": 1790190000},
				},
			})
		})
	}

	queries = func() []string {
		b, err := os.ReadFile(logFile)
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		return strings.Split(strings.TrimSuffix(string(b), "\n---\n"), "\n---\n")
	}
	return p, queries
}

var mariadbVars = map[string]string{
	"version":               "11.4.2-MariaDB-log",
	"innodb_file_per_table": "ON",
	"wsrep_on":              "OFF",
	"tmpdir":                "/tmp",
}

// optimizeTables are the tables returned by the fake server. Thresholds are applied by the query, so they are not
// tested here.
var optimizeTables = [][]any{
	{"main", "logs", 200 * mib, 50 * mib, 300 * mib, 0, 0},
	{"main", "cache", 2048 * mib, 1024 * mib, 1024 * mib, 0, 0},
	{"main", "search", 100 * mib, 100 * mib, 150 * mib, 1, 0},
	{"main", "we`ird", 100 * mib, 0, 120 * mib, 0, 1},
}

func TestDbMysqlOptimizeReport(t *testing.T) {
	t.Parallel()
	p, queries := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: mariadbVars, tables: optimizeTables}, true)
	args := func(extra ...string) []string {
		return append([]string{"db:mysql-optimize", "-p", layoutProjectID, "-e", "main", "-r", "database"}, extra...)
	}

	output, stdErr, err := p.f.RunCombinedOutput(args("--format", "csv",
		"--columns", "schema,table,size,reclaimable,percent,disk_needed,disk_fits,tmp_needed,tmp_fits")...)
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "Checking database service db...")
	assert.Contains(t, stdErr, "Disk: 3.0 GiB free of 4.0 GiB. /tmp: 924.0 MiB free of 1.0 GiB.")
	assert.Equal(t, `Schema,Table,Size,Reclaimable,Reclaimable %,Disk needed,Fits disk,/tmp needed,Fits /tmp
main,logs,262144000,314572800,120%,262144000,true,52428800,true
main,cache,3221225472,1073741824,33%,3221225472,false,1073741824,false
main,search,209715200,157286400,75%,209715200,true,104857600,true
main,we`+"`"+`ird,104857600,125829120,120%,104857600,true,0,true
`, output)

	output = p.f.Run(args("--format", "tsv", "--no-header", "--columns", "table,notes")...)
	assert.Equal(t, "logs\t\ncache\t\n"+
		"search\thas a FULLTEXT or SPATIAL index: it cannot be rebuilt without blocking writes\n"+
		"we`ird\tprobably in a shared tablespace: a rebuild would not reclaim space\n", output)

	q := queries()
	require.Len(t, q, 4) // Two runs.
	assert.Contains(t, q[0], "SHOW GLOBAL VARIABLES WHERE Variable_name IN (")
	assert.Contains(t, q[0], "SHOW GLOBAL STATUS WHERE Variable_name IN ('wsrep_cluster_size')")
	// MariaDB has no information_schema_stats_expiry.
	assert.NotContains(t, q[1], "information_schema_stats_expiry")
	assert.Contains(t, q[1], "t.DATA_FREE >= 104857600 AND t.DATA_FREE * 100 >= 20 * (t.DATA_LENGTH + t.INDEX_LENGTH)")

	// Thresholds and table filters, with escaped strings.
	_, stdErr, err = p.f.RunCombinedOutput(args("--min-reclaimable", "1G", "--min-percent", "50",
		"--table", "main.logs", "-t", "o'ther")...)
	require.NoError(t, err, stdErr)
	q = queries()
	assert.Contains(t, q[len(q)-1], "t.DATA_FREE >= 1073741824 AND t.DATA_FREE * 100 >= 50 *")
	assert.Contains(t, q[len(q)-1], "((t.TABLE_SCHEMA = 'main' AND t.TABLE_NAME = 'logs') OR t.TABLE_NAME = 'o''ther')")
	assert.Contains(t, stdErr, "To rebuild these tables, run this command again with --execute.")

	// The default table output.
	output = p.f.Run(args("--columns", "table,size,disk_fits")...)
	assert.Contains(t, output, "| logs   | 250.0 MiB | yes       |")
	assert.Contains(t, output, "| cache  | 3.0 GiB   | no        |")

	_, stdErr, err = p.f.RunCombinedOutput(args("--min-percent", "x")...)
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Invalid --min-percent value: x")
	_, stdErr, err = p.f.RunCombinedOutput(args("--min-reclaimable", "lots")...)
	assert.Error(t, err)
	assert.Contains(t, stdErr, "Invalid size: lots")
}

func TestDbMysqlOptimizeExecute(t *testing.T) {
	t.Parallel()
	p, queries := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: mariadbVars, tables: optimizeTables}, true)
	args := []string{"db:mysql-optimize", "-p", layoutProjectID, "-e", "main", "-r", "database", "--execute"}

	alters := func() []string {
		var a []string
		for _, q := range queries() {
			if strings.HasPrefix(q, "ALTER TABLE") {
				a = append(a, q)
			}
		}
		return a
	}

	// "cache" does not fit, so nothing is rebuilt.
	_, stdErr, err := p.f.RunCombinedOutput(args...)
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "Skipping main.search: has a FULLTEXT or SPATIAL index")
	assert.Contains(t, stdErr, "Skipping main.we`ird: probably in a shared tablespace")
	assert.Contains(t, stdErr,
		"Error: There is not enough free disk space to rebuild main.cache (it needs about 3.0 GiB).")
	assert.Contains(t, stdErr, "Error: There is not enough free space in /tmp to rebuild main.cache")
	assert.Contains(t, stdErr, "The tables were not rebuilt.")
	assert.Empty(t, alters())

	// A table that fits is rebuilt.
	_, stdErr, err = p.f.RunCombinedOutput(append(args, "--table", "main.logs")...)
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "Rebuilding main.logs... [OK]")
	assert.Equal(t, []string{"ALTER TABLE `main`.`logs` FORCE, ALGORITHM=INPLACE, LOCK=NONE;"}, alters())

	// With --force, tables that do not fit are rebuilt too, with warnings.
	_, stdErr, err = p.f.RunCombinedOutput(append(args, "--force")...)
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "Warning: There is not enough free disk space to rebuild main.cache")
	assert.Equal(t, []string{
		"ALTER TABLE `main`.`logs` FORCE, ALGORITHM=INPLACE, LOCK=NONE;",
		"ALTER TABLE `main`.`logs` FORCE, ALGORITHM=INPLACE, LOCK=NONE;",
		"ALTER TABLE `main`.`cache` FORCE, ALGORITHM=INPLACE, LOCK=NONE;",
	}, alters())

	_, stdErr, err = p.f.RunCombinedOutput("db:mysql-optimize", "-p", layoutProjectID, "-e", "main", "--force")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The --force option requires --execute.")
}

func TestDbMysqlOptimizeEscaping(t *testing.T) {
	t.Parallel()
	tables := [][]any{{"my`db", "we`ird", 100 * mib, 0, 120 * mib, 0, 0}}
	p, queries := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: mariadbVars, tables: tables}, true)

	_, stdErr, err := p.f.RunCombinedOutput("db:mysql-optimize", "-p", layoutProjectID, "-e", "main",
		"-r", "database", "--execute", "--analyze")
	require.NoError(t, err, stdErr)
	q := queries()
	require.Len(t, q, 5)
	assert.Equal(t, "ANALYZE TABLE `my``db`.`we``ird`;", q[2])
	assert.Equal(t, "ALTER TABLE `my``db`.`we``ird` FORCE, ALGORITHM=INPLACE, LOCK=NONE;", q[4])
}

func TestDbMysqlOptimizeGalera(t *testing.T) {
	t.Parallel()
	vars := map[string]string{
		"version":               "10.11.8-MariaDB-log",
		"innodb_file_per_table": "ON",
		"wsrep_on":              "ON",
		"wsrep_cluster_size":    "3",
	}
	p, queries := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: vars, tables: optimizeTables[:1]}, true)
	args := []string{"db:mysql-optimize", "-p", layoutProjectID, "-e", "main", "-r", "database"}

	_, stdErr, err := p.f.RunCombinedOutput(args...)
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "Warning: the database is a Galera cluster (3 nodes)")

	_, stdErr, err = p.f.RunCombinedOutput(append(args, "--execute")...)
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "Error: The database is a Galera cluster")

	_, stdErr, err = p.f.RunCombinedOutput(append(args, "--execute", "--force")...)
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "Warning: The database is a Galera cluster")
	q := queries()
	assert.Equal(t, "ALTER TABLE `main`.`logs` FORCE, ALGORITHM=INPLACE, LOCK=NONE;", q[len(q)-1])
}

func TestDbMysqlOptimizeHeadroomUnknown(t *testing.T) {
	t.Parallel()
	// MySQL 8 caches statistics, and innodb_file_per_table is OFF.
	vars := map[string]string{
		"version":                         "8.0.36",
		"version_comment":                 "MySQL Community Server - GPL",
		"innodb_file_per_table":           "OFF",
		"information_schema_stats_expiry": "86400",
	}
	tables := [][]any{
		{"main", "logs", 200 * mib, 50 * mib, 300 * mib, 0, 0},
		{"main", "other", 200 * mib, 50 * mib, 300 * mib, 0, 0},
	}
	for _, metrics := range []string{"unavailable", "disabled"} {
		t.Run(metrics, func(t *testing.T) {
			t.Parallel()
			p, queries := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: vars, tables: tables}, false)
			if metrics == "disabled" {
				p.f.extraEnv = append(p.f.extraEnv, EnvPrefix+"API_METRICS=0")
			}
			args := []string{"db:mysql-optimize", "-p", layoutProjectID, "-e", "main", "-r", "database"}

			output, stdErr, err := p.f.RunCombinedOutput(append(args, "--format", "csv", "--no-header",
				"--columns", "table,disk_fits,tmp_fits")...)
			require.NoError(t, err, stdErr)
			assert.Contains(t, stdErr, "Warning: innodb_file_per_table is OFF")
			assert.Contains(t, stdErr, "Warning: the disk usage of the service is unknown")
			assert.Equal(t, "logs,,\nother,,\n", output)
			q := queries()
			assert.True(t, strings.HasPrefix(q[1], "SET SESSION information_schema_stats_expiry = 0;\n"), q[1])

			// All tables are skipped, as they are in the shared tablespace.
			_, stdErr, err = p.f.RunCombinedOutput(append(args, "--execute", "--force")...)
			assertExitCode(t, 1, err)
			assert.Contains(t, stdErr, "Skipping main.logs: probably in a shared tablespace")
			assert.Contains(t, stdErr, "No tables can be rebuilt.")
		})
	}

	// With file-per-table, an unknown headroom prevents a rebuild without --force.
	p, _ := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: mariadbVars, tables: tables[:1]}, false)
	_, stdErr, err := p.f.RunCombinedOutput("db:mysql-optimize", "-p", layoutProjectID, "-e", "main",
		"-r", "database", "--execute")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "Error: The disk usage of the service is unknown.")
}

func TestDbMysqlOptimizeOtherEngines(t *testing.T) {
	t.Parallel()
	p, queries := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: mariadbVars}, true)

	_, stdErr, err := p.f.RunCombinedOutput("db:mysql-optimize", "-p", layoutProjectID, "-e", "main", "-r", "pg")
	assertExitCode(t, 1, err)
	assert.Contains(t, stdErr, "The relationship pg is not a MySQL or MariaDB database (its scheme is: pgsql).")
	assert.Empty(t, queries())

	// Without --relationship, the MySQL relationship is chosen.
	_, stdErr, err = p.f.RunCombinedOutput("db:mysql-optimize", "-p", layoutProjectID, "-e", "main")
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "No tables were found with at least 100.0 MiB and 20% of their size reclaimable.")
}

func TestDbSizeCleanupDeprecated(t *testing.T) {
	t.Parallel()
	p := setUpDBProject(t, map[string]string{"mariadb": "cat > /dev/null; echo 52428800"})
	_, stdErr, err := p.f.RunCombinedOutput("db:size", "-p", layoutProjectID, "-e", "main", "-r", "database",
		"--cleanup")
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "The --cleanup option is deprecated")
	assert.Contains(t, stdErr, "platform-test db:mysql-optimize")
}
