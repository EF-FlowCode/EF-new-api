package mysqlversion

import "testing"

func TestRequire8(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "MySQL 8.0", version: "8.0.36", wantErr: false},
		{name: "MySQL 8.4", version: "8.4.3", wantErr: false},
		{name: "MySQL packaged build", version: "8.0.36-0ubuntu0.22.04.1", wantErr: false},
		{name: "MySQL 9", version: "9.1.0", wantErr: true},
		{name: "MySQL 5.7", version: "5.7.44", wantErr: true},
		{name: "MariaDB", version: "10.11.6-MariaDB", wantErr: true},
		{name: "MariaDB compatibility prefix", version: "5.5.5-10.11.6-MariaDB", wantErr: true},
		{name: "empty", version: "", wantErr: true},
		{name: "major only", version: "8", wantErr: true},
		{name: "missing patch", version: "8.0", wantErr: true},
		{name: "missing minor and patch", version: "8.", wantErr: true},
		{name: "non numeric", version: "8.development", wantErr: true},
		{name: "Vitess suffix", version: "8.0.36-Vitess", wantErr: true},
		{name: "invalid", version: "development", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Require8(test.version)
			if (err != nil) != test.wantErr {
				t.Fatalf("Require8(%q) error = %v, wantErr %v", test.version, err, test.wantErr)
			}
		})
	}
}

func TestRequire8RejectsCompatibleServerComments(t *testing.T) {
	for _, comment := range []string{
		"MariaDB Server",
		"Percona Server (GPL)",
		"TiDB Server",
		"Vitess MySQL-compatible server",
		"OceanBase",
		"PolarDB for MySQL",
		"AliSQL Server",
	} {
		t.Run(comment, func(t *testing.T) {
			if err := require8("8.0.36", comment); err == nil {
				t.Fatalf("expected compatible server comment %q to be rejected", comment)
			}
		})
	}
}

func TestRequire8ServerAcceptsOnlyOracleIdentity(t *testing.T) {
	tests := []struct {
		name           string
		versionComment string
		wantErr        bool
	}{
		{name: "community", versionComment: "MySQL Community Server - GPL"},
		{name: "enterprise", versionComment: "MySQL Enterprise Server - Commercial"},
		{name: "unknown compatible", versionComment: "Acme SQL Server", wantErr: true},
		{name: "empty identity", versionComment: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := requireOracle8("8.4.8", test.versionComment)
			if (err != nil) != test.wantErr {
				t.Fatalf("requireOracle8() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
