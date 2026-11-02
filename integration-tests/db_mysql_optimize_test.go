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

// fakeTable is a table returned by the fake server.
type fakeTable struct {
	schema, name                      string
	dataLength, indexLength, dataFree int
	rows, offlineIndexes, sameFree    int
	// Whether the primary key is not a single integer column, so the first rows are sampled.
	noIntegerKey bool
	// The average bytes of the sampled rows.
	avgRowBytes int
}

// fakeMysqlServer is a fake "mariadb" client, which answers the queries of db:mysql-optimize.
type fakeMysqlServer struct {
	vars       map[string]string
	tables     []fakeTable
	failSample bool
}

// script returns the fake client, which logs each query to a file in dir.
func (s fakeMysqlServer) script(t *testing.T, dir string) string {
	var vars, tables, columns, samples strings.Builder
	for k, v := range s.vars {
		fmt.Fprintf(&vars, "%s\t%s\n", k, v)
	}
	quote := func(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
	for _, tbl := range s.tables {
		fmt.Fprintf(&tables, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\n", tbl.schema, tbl.name,
			tbl.dataLength, tbl.indexLength, tbl.dataFree, tbl.rows, tbl.offlineIndexes, tbl.sameFree)
		// Columns are matched by the table's name in the query, as "('schema', 'name')".
		key := "PRI"
		if tbl.noIntegerKey {
			key = ""
		}
		match := fmt.Sprintf("('%s', '%s')", tbl.schema, tbl.name)
		fmt.Fprintf(&columns, "%s\t%s\t%s\tid\tint\t%s\n", match, tbl.schema, tbl.name, key)
		fmt.Fprintf(&columns, "%s\t%s\t%s\tpayload\tvarchar\t\n", match, tbl.schema, tbl.name)
		fmt.Fprintf(&samples, "%s.%s\t1000\t%d\n", quote(tbl.schema), quote(tbl.name), 1000*tbl.avgRowBytes)
	}
	for name, b := range map[string]*strings.Builder{"vars": &vars, "tables": &tables, "columns": &columns,
		"samples": &samples} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name+".tsv"), []byte(b.String()), 0o600))
	}
	return fmt.Sprintf(`dir=%q
failSample=%t
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
  *"FROM information_schema.COLUMNS"*)
    # The last field may be empty, so the lines are not split with read.
    while IFS= read -r line; do
      case "$q" in *"${line%%%%$'\t'*}"*) printf '%%s\n' "${line#*$'\t'}" ;; esac
    done < "$dir/columns.tsv" ;;
  *"COALESCE(SUM(b), 0)"*)
    if $failSample; then echo "ERROR 1142 (42000): SELECT command denied" >&2; exit 1; fi
    # One line per table: answer with its index.
    while IFS= read -r line; do
      [[ $line =~ SELECT\ ([0-9]+),\ COUNT ]] || continue
      while IFS=$'\t' read -r quoted n bytes; do
        case "$line" in *"FROM $quoted "*) printf '%%s\t%%s\t%%s\n' "${BASH_REMATCH[1]}" "$n" "$bytes" ;; esac
      done < "$dir/samples.tsv"
    done <<< "$q" ;;
  *) echo "unexpected query: $q" >&2; exit 1 ;;
esac
`, dir, s.failSample)
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

// optimizeTables are the tables returned by the fake server. The size threshold is applied by the query, so the fake
// server ignores it.
var optimizeTables = []fakeTable{
	// Rebuilt: 75 MiB of data, and 18.75 MiB of indexes.
	{schema: "main", name: "logs", dataLength: 200 * mib, indexLength: 50 * mib, dataFree: 12 * mib,
		rows: 102400, avgRowBytes: 512},
	// Rebuilt: 3 GiB of data, and the indexes do not grow: only the free extents are reclaimable.
	{schema: "main", name: "cache", dataLength: 2048 * mib, indexLength: 1024 * mib, dataFree: 1024 * mib,
		rows: 1048576, avgRowBytes: 2048},
	// Rebuilt: 3 MiB of data, and 3 MiB of indexes.
	{schema: "main", name: "search", dataLength: 100 * mib, indexLength: 100 * mib, dataFree: 150 * mib,
		rows: 2048, avgRowBytes: 1024, offlineIndexes: 1, noIntegerKey: true},
	// Not sampled.
	{schema: "main", name: "we`ird", dataLength: 100 * mib, dataFree: 120 * mib, sameFree: 1},
	// Rebuilt: 112.5 MiB of data, so less than 100 MiB is reclaimable.
	{schema: "main", name: "small", dataLength: 150 * mib, rows: 153600, avgRowBytes: 512},
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
	assert.Contains(t, stdErr, "Sampling rows from 4 table(s)...")
	assert.Contains(t, stdErr, "Disk: 3.0 GiB free of 4.0 GiB. /tmp: 924.0 MiB free of 1.0 GiB.")
	// Sorted by reclaimable space. The disk needed is the rebuilt size, plus 10% and 8 MiB.
	assert.Equal(t, `Schema,Table,Size,Reclaimable,Reclaimable %,Disk needed,Fits disk,/tmp needed,Fits /tmp
main,cache,3221225472,1073741824,33%,4732852633,false,1073741824,false
main,search,209715200,360710144,172%,15309209,true,104857600,true
main,logs,262144000,176422912,67%,116523008,true,52428800,true
main,we`+"`"+`ird,104857600,125829120,120%,104857600,true,0,true
`, output)

	output = p.f.Run(args("--format", "tsv", "--no-header", "--columns", "table,notes")...)
	assert.Equal(t, "cache\t\n"+
		"search\thas a FULLTEXT or SPATIAL index: it cannot be rebuilt without blocking writes;"+
		" estimated from the first rows only\n"+
		"logs\t\n"+
		"we`ird\tprobably in a shared tablespace: a rebuild would not reclaim space\n", output)

	q := queries()
	require.Len(t, q, 8) // Two runs.
	assert.Contains(t, q[0], "SHOW GLOBAL VARIABLES WHERE Variable_name IN (")
	assert.Contains(t, q[0], "SHOW GLOBAL STATUS WHERE Variable_name IN ('wsrep_cluster_size')")
	// MariaDB has no information_schema_stats_expiry.
	assert.NotContains(t, q[1], "information_schema_stats_expiry")
	assert.Contains(t, q[1], "t.DATA_LENGTH + t.INDEX_LENGTH + t.DATA_FREE >= 104857600")
	// Tables in a shared tablespace are not sampled.
	assert.Contains(t, q[2], "FROM information_schema.COLUMNS WHERE (TABLE_SCHEMA, TABLE_NAME) IN (('main', 'logs'), ")
	assert.NotContains(t, q[2], "we`ird")
	assert.Contains(t, q[3], "SELECT MIN(`id`), MAX(`id`) FROM `main`.`logs` INTO @lo, @hi;")
	assert.Contains(t, q[3], "FROM `main`.`search` LIMIT 1000) s;")
	assert.NotContains(t, q[3], "we``ird")

	// Thresholds and table filters, with escaped strings.
	_, stdErr, err = p.f.RunCombinedOutput(args("--min-reclaimable", "150M", "--min-percent", "50",
		"--table", "main.logs", "-t", "o'ther")...)
	require.NoError(t, err, stdErr)
	q = queries()
	assert.Contains(t, q[len(q)-3], "t.DATA_LENGTH + t.INDEX_LENGTH + t.DATA_FREE >= 157286400")
	assert.Contains(t, q[len(q)-3], "((t.TABLE_SCHEMA = 'main' AND t.TABLE_NAME = 'logs') OR t.TABLE_NAME = 'o''ther')")
	assert.Contains(t, stdErr, "To rebuild these tables, run this command again with --execute.")

	// The percentage is applied after sampling.
	_, stdErr, err = p.f.RunCombinedOutput(args("--min-percent", "80", "--table", "main.logs")...)
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "No tables were found with at least 100.0 MiB and 80% of their size reclaimable.")

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

func TestDbMysqlOptimizeSampleFailure(t *testing.T) {
	t.Parallel()
	p, _ := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: mariadbVars, tables: optimizeTables, failSample: true},
		true)

	// Only the free extents are reclaimable, and a rebuild needs the table's size.
	output, stdErr, err := p.f.RunCombinedOutput("db:mysql-optimize", "-p", layoutProjectID, "-e", "main",
		"-r", "database", "--format", "csv", "--no-header", "--columns", "table,reclaimable,disk_needed")
	require.NoError(t, err, stdErr)
	assert.Contains(t, stdErr, "Warning: failed to sample rows: only free extents are counted as reclaimable.")
	assert.Equal(t, "cache,1073741824,3221225472\nsearch,157286400,209715200\nwe`ird,125829120,104857600\n", output)
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
		"Error: There is not enough free disk space to rebuild main.cache (it needs about 4.4 GiB).")
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
		"ALTER TABLE `main`.`cache` FORCE, ALGORITHM=INPLACE, LOCK=NONE;",
		"ALTER TABLE `main`.`logs` FORCE, ALGORITHM=INPLACE, LOCK=NONE;",
	}, alters())

	_, stdErr, err = p.f.RunCombinedOutput("db:mysql-optimize", "-p", layoutProjectID, "-e", "main", "--force")
	assert.Error(t, err)
	assert.Contains(t, stdErr, "The --force option requires --execute.")
}

func TestDbMysqlOptimizeEscaping(t *testing.T) {
	t.Parallel()
	tables := []fakeTable{{schema: "my`db", name: "we`ird", dataLength: 100 * mib, dataFree: 120 * mib,
		rows: 1024, avgRowBytes: 100}}
	p, queries := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: mariadbVars, tables: tables}, true)

	_, stdErr, err := p.f.RunCombinedOutput("db:mysql-optimize", "-p", layoutProjectID, "-e", "main",
		"-r", "database", "--execute", "--analyze")
	require.NoError(t, err, stdErr)
	q := queries()
	require.Len(t, q, 9)
	assert.Contains(t, q[2], "IN (('my`db', 'we`ird'))")
	assert.Contains(t, q[3], "FROM `my``db`.`we``ird` INTO @lo, @hi;")
	assert.Equal(t, "ANALYZE TABLE `my``db`.`we``ird`;", q[4])
	assert.Equal(t, "ALTER TABLE `my``db`.`we``ird` FORCE, ALGORITHM=INPLACE, LOCK=NONE;", q[8])
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
	tables := []fakeTable{
		{schema: "main", name: "logs", dataLength: 200 * mib, indexLength: 50 * mib, dataFree: 300 * mib},
		{schema: "main", name: "other", dataLength: 200 * mib, indexLength: 50 * mib, dataFree: 300 * mib},
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
			// Tables in the shared tablespace are not sampled.
			q := queries()
			require.Len(t, q, 2)
			assert.True(t, strings.HasPrefix(q[1], "SET SESSION information_schema_stats_expiry = 0;\n"), q[1])

			// All tables are skipped, as they are in the shared tablespace.
			_, stdErr, err = p.f.RunCombinedOutput(append(args, "--execute", "--force")...)
			assertExitCode(t, 1, err)
			assert.Contains(t, stdErr, "Skipping main.logs: probably in a shared tablespace")
			assert.Contains(t, stdErr, "No tables can be rebuilt.")
		})
	}

	// With file-per-table, an unknown headroom prevents a rebuild without --force.
	p, _ := setUpMysqlOptimizeProject(t, fakeMysqlServer{vars: mariadbVars, tables: optimizeTables[:1]}, false)
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
