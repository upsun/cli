package commands

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/upsun/cli/internal/api"
	"github.com/upsun/cli/internal/auth"
	"github.com/upsun/cli/internal/config"
	"github.com/upsun/cli/internal/tableoutput"
)

// Column definitions for project list
type projectColumn struct {
	Header string
	Key    string
	Get    func(p *api.ProjectInfo) string
}

var allProjectColumns = []projectColumn{
	{Header: "ID", Key: "id", Get: func(p *api.ProjectInfo) string { return p.ID }},
	{Header: "Title", Key: "title", Get: func(p *api.ProjectInfo) string {
		if p.Title == "" {
			return "[Untitled Project]"
		}
		return p.Title
	}},
	{Header: "Region", Key: "region", Get: func(p *api.ProjectInfo) string { return p.Region }},
	{Header: "Org name", Key: "organization_name", Get: func(p *api.ProjectInfo) string {
		if p.OrganizationRef != nil {
			return p.OrganizationRef.Name
		}
		return ""
	}},
	{Header: "Org ID", Key: "organization_id", Get: func(p *api.ProjectInfo) string {
		if p.OrganizationRef != nil {
			return p.OrganizationRef.ID
		}
		return p.OrganizationID
	}},
	{Header: "Org label", Key: "organization_label", Get: func(p *api.ProjectInfo) string {
		if p.OrganizationRef != nil {
			return p.OrganizationRef.Label
		}
		return ""
	}},
	{Header: "Org type", Key: "organization_type", Get: func(p *api.ProjectInfo) string {
		if p.OrganizationRef != nil {
			return p.OrganizationRef.Type
		}
		return ""
	}},
	{Header: "Status", Key: "status", Get: func(p *api.ProjectInfo) string { return p.Status }},
	{Header: "Created", Key: "created_at", Get: func(p *api.ProjectInfo) string {
		return p.CreatedAt.Format("2006-01-02 15:04:05")
	}},
}

func getDefaultProjectColumns(cnf *config.Config) []string {
	cols := []string{"id", "title", "region"}
	if cnf.API.EnableOrganizations {
		cols = append(cols, "organization_name", "organization_type")
	}
	return cols
}

func getProjectColumn(key string) *projectColumn {
	for i := range allProjectColumns {
		if allProjectColumns[i].Key == key {
			return &allProjectColumns[i]
		}
	}
	return nil
}

func newProjectListCommand(cnf *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project:list",
		Aliases: []string{"projects", "pro"},
		Short:   "Get a list of all active projects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProjectList(cmd, cnf)
		},
	}

	cmd.Flags().Bool("pipe", false, "Output a simple list of project IDs. Disables pagination.")
	cmd.Flags().String("region", "", "Filter by region (exact match)")
	cmd.Flags().String("title", "", "Filter by title (case-insensitive search)")
	cmd.Flags().Bool("my", false, "Display only the projects you own")
	cmd.Flags().Int("refresh", 1, "Whether to refresh the list")
	cmd.Flags().String("sort", "title", "A property to sort by")
	cmd.Flags().Bool("reverse", false, "Sort in reverse (descending) order")
	cmd.Flags().Int("page", 0, "Page number. This enables pagination.")
	cmd.Flags().IntP("count", "c", 0, "The number of projects to display per page. Use 0 to disable pagination.")

	if cnf.API.EnableOrganizations {
		cmd.Flags().StringP("org", "o", "", "Filter by organization name or ID")
		cmd.Flags().String("org-type", "", "Filter by organization type")
	}

	cmd.Flags().String("format", "table", "The output format: table, plain, csv, or tsv")
	cmd.Flags().String("columns", "", "Columns to display (comma-separated)")
	cmd.Flags().Bool("no-header", false, "Do not output the table header")

	_ = viper.BindPFlags(cmd.Flags())

	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		cmd.Root().Run(cmd.Root(), append([]string{"help", "project:list"}, args...))
	})

	return cmd
}

func runProjectList(cmd *cobra.Command, cnf *config.Config) error {
	ctx := cmd.Context()

	// Create the legacy CLI client for authentication
	legacyCLIClient, err := auth.NewLegacyCLIClient(ctx,
		makeLegacyCLIWrapper(cnf, cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin()))
	if err != nil {
		return err
	}

	if err := legacyCLIClient.EnsureAuthenticated(ctx); err != nil {
		return err
	}

	apiClient, err := api.NewClient(cnf.API.BaseURL, legacyCLIClient.HTTPClient)
	if err != nil {
		return err
	}

	// Show loading message
	fmt.Fprint(cmd.ErrOrStderr(), "Loading projects...\r")

	// Fetch projects
	projects, err := apiClient.GetMyProjects(ctx)
	if err != nil {
		return err
	}

	// Clear the loading message
	fmt.Fprint(cmd.ErrOrStderr(), "                   \r")

	// Apply filters
	projects = filterProjects(cmd, projects, cnf, apiClient)

	// Sort projects
	sortKey, _ := cmd.Flags().GetString("sort")
	sortProjects(projects, sortKey)

	reverse, _ := cmd.Flags().GetBool("reverse")
	if reverse {
		slices.Reverse(projects)
	}

	// Handle --pipe output
	pipe, _ := cmd.Flags().GetBool("pipe")
	if pipe {
		for _, p := range projects {
			fmt.Fprintln(cmd.OutOrStdout(), p.ID)
		}
		return nil
	}

	// Check if no projects found and display appropriate message
	if len(projects) == 0 {
		filtersInUse := getFiltersInUse(cmd, cnf)
		if len(filtersInUse) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "No projects found (filters in use: %s).\n", strings.Join(filtersInUse, ", "))
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "No projects found.")
		}
		return nil
	}

	// Determine columns to display
	columns := getSelectedColumns(cmd, cnf)

	// Build table
	table := buildProjectTable(projects, columns)

	// Render output
	return renderOutput(cmd, table)
}

func filterProjects(
	cmd *cobra.Command,
	projects []*api.ProjectInfo,
	cnf *config.Config,
	apiClient *api.Client,
) []*api.ProjectInfo {
	ctx := cmd.Context()
	result := projects

	// Filter by region
	region, _ := cmd.Flags().GetString("region")
	if region != "" {
		result = slices.DeleteFunc(result, func(p *api.ProjectInfo) bool {
			return !strings.EqualFold(p.Region, region)
		})
	}

	// Filter by title
	title, _ := cmd.Flags().GetString("title")
	if title != "" {
		titleLower := strings.ToLower(title)
		result = slices.DeleteFunc(result, func(p *api.ProjectInfo) bool {
			return !strings.Contains(strings.ToLower(p.Title), titleLower)
		})
	}

	// Filter by --my (ownership)
	my, _ := cmd.Flags().GetBool("my")
	if my {
		myUserID, err := apiClient.GetMyUserID(ctx)
		if err == nil && myUserID != "" {
			if cnf.API.EnableOrganizations {
				result = slices.DeleteFunc(result, func(p *api.ProjectInfo) bool {
					if p.OrganizationRef != nil {
						return p.OrganizationRef.OwnerID != myUserID
					}
					return true
				})
			}
		}
	}

	// Filter by organization
	if cnf.API.EnableOrganizations {
		org, _ := cmd.Flags().GetString("org")
		if org != "" {
			result = slices.DeleteFunc(result, func(p *api.ProjectInfo) bool {
				if p.OrganizationRef == nil {
					return true
				}
				return p.OrganizationRef.ID != org && p.OrganizationRef.Name != org
			})
		}

		orgType, _ := cmd.Flags().GetString("org-type")
		if orgType != "" {
			result = slices.DeleteFunc(result, func(p *api.ProjectInfo) bool {
				if p.OrganizationRef == nil {
					return true
				}
				return p.OrganizationRef.Type != orgType
			})
		}
	}

	return result
}

func getFiltersInUse(cmd *cobra.Command, cnf *config.Config) []string {
	var filters []string

	if region, _ := cmd.Flags().GetString("region"); region != "" {
		filters = append(filters, "--region")
	}
	if title, _ := cmd.Flags().GetString("title"); title != "" {
		filters = append(filters, "--title")
	}
	if my, _ := cmd.Flags().GetBool("my"); my {
		filters = append(filters, "--my")
	}
	if cnf.API.EnableOrganizations {
		if org, _ := cmd.Flags().GetString("org"); org != "" {
			filters = append(filters, "--org")
		}
		if orgType, _ := cmd.Flags().GetString("org-type"); orgType != "" {
			filters = append(filters, "--org-type")
		}
	}

	return filters
}

func sortProjects(projects []*api.ProjectInfo, sortKey string) {
	slices.SortFunc(projects, func(a, b *api.ProjectInfo) int {
		switch sortKey {
		case "id":
			return cmp.Compare(a.ID, b.ID)
		case "region":
			return cmp.Compare(a.Region, b.Region)
		case "created_at":
			return a.CreatedAt.Compare(b.CreatedAt)
		default: // title
			return cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		}
	})
}

func getSelectedColumns(cmd *cobra.Command, cnf *config.Config) []projectColumn {
	columnsStr, _ := cmd.Flags().GetString("columns")
	var selectedKeys []string

	if columnsStr != "" {
		selectedKeys = strings.Split(columnsStr, ",")
		for i, k := range selectedKeys {
			selectedKeys[i] = strings.TrimSpace(k)
		}
	} else {
		selectedKeys = getDefaultProjectColumns(cnf)
	}

	columns := make([]projectColumn, 0, len(selectedKeys))
	for _, key := range selectedKeys {
		if col := getProjectColumn(key); col != nil {
			columns = append(columns, *col)
		}
	}

	return columns
}

func buildProjectTable(projects []*api.ProjectInfo, columns []projectColumn) *tableoutput.Table {
	headers := make([]string, len(columns))
	for i, col := range columns {
		headers[i] = col.Header
	}

	table := tableoutput.New(headers...)
	for _, p := range projects {
		row := make([]string, len(columns))
		for i, col := range columns {
			row[i] = col.Get(p)
		}
		table.AddRow(row...)
	}

	return table
}

func renderOutput(cmd *cobra.Command, table *tableoutput.Table) error {
	format, _ := cmd.Flags().GetString("format")
	noHeader, _ := cmd.Flags().GetBool("no-header")
	w := cmd.OutOrStdout()

	switch format {
	case "csv":
		return table.RenderCSV(w, noHeader)
	case "tsv", "plain":
		return table.RenderPlain(w)
	default: // table
		return table.RenderTable(w)
	}
}
