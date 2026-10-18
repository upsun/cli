package platformify

// testInputs returns template inputs for each stack, without assets.
func testInputs() map[string]*templateInput {
	return map[string]*templateInput{
		"django": {
			Name:            "django",
			Type:            "python:3.14",
			Stack:           django,
			Runtime:         "python",
			ApplicationRoot: "app",
			Environment: map[string]string{
				"DJANGO_SETTINGS_MODULE": "app.settings",
				"PYTHONUNBUFFERED":       "1",
			},
			BuildFlavor: "none",
			BuildSteps: []string{
				"pip install -r requirements.txt",
				"# comment here",
				"python manage.py collectstatic --noinput",
			},
			WebCommand:   "gunicorn app.wsgi",
			SocketFamily: "unix",
			DeployCommand: []string{
				"python manage.py migrate",
				"# comment here",
			},
			Locations: map[string]map[string]any{
				"/":       {"passthru": true},
				"/static": {"root": "static", "expires": "1h", "allow": true},
			},
			Dependencies: map[string]map[string]string{
				"python": {"poetry": "*", "pip": ">=20.0.0"},
				"nodejs": {"yarn": "*", "npm": ">=6.0.0"},
			},
			Disk: "1024",
			Mounts: map[string]map[string]string{
				"/.npm": {"source": "local", "source_path": "npm"},
				"/.pip": {"source": "local", "source_path": "pip"},
			},
			Relationships: map[string]relationship{
				"db":    {Service: "db", Endpoint: "postgresql"},
				"mysql": {Service: "mysql", Endpoint: "mysql"},
			},
			Services: []service{
				{
					Name:         "db",
					Type:         "postgresql:17",
					TypeVersions: []string{"17", "16", "15", "14", "13", "12"},
					Disk:         "1024",
					DiskSizes:    []string{"1024", "2048"},
				},
				{
					Name:         "mysql",
					Type:         "mysql:11.8",
					TypeVersions: []string{"11.0", "10.11", "10.6", "10.5", "10.4", "10.3"},
					Disk:         "1024",
					DiskSizes:    []string{"1024", "2034"},
				},
			},
		},
		"generic": {
			Name:    "generic",
			Type:    "java:21",
			Runtime: "java",
			Stack:   genericStack,
			Environment: map[string]string{
				"JAVA": "19",
			},
			BuildSteps:   []string{"mvn install"},
			WebCommand:   "tomcat",
			SocketFamily: "tcp",
			Locations: map[string]map[string]any{
				"/": {"passthru": true},
			},
			Dependencies: map[string]map[string]string{},
			Disk:         "1024",
			Mounts: map[string]map[string]string{
				"/.mvn": {"source": "local", "source_path": "maven"},
			},
			Relationships: map[string]relationship{
				"mysql": {Service: "mysql", Endpoint: "mysql"},
			},
			Services: []service{
				{
					Name:         "mysql",
					Type:         "mysql:11.8",
					TypeVersions: []string{"13", "14", "15"},
					Disk:         "1024",
					DiskSizes:    []string{"1024", "2048"},
				},
			},
		},
		"laravel": {
			Name:            "laravel",
			Type:            "php:8.4",
			Stack:           laravel,
			Runtime:         "php",
			ApplicationRoot: "app",
			Environment:     map[string]string{},
			BuildFlavor:     "php",
			Locations: map[string]map[string]any{
				"/": {"root": "index.php"},
			},
			Dependencies:  map[string]map[string]string{},
			Mounts:        map[string]map[string]string{},
			Relationships: map[string]relationship{},
		},
		"nextjs":  {Name: "nextjs", Type: "nodejs:24", Runtime: "nodejs", Stack: nextJS},
		"strapi":  {Name: "strapi", Type: "nodejs:24", Runtime: "nodejs", Stack: strapi},
		"flask":   {Name: "flask", Type: "python:3.14", Runtime: "python", Stack: flask},
		"express": {Name: "express", Type: "nodejs:24", Runtime: "nodejs", Stack: express},
	}
}
