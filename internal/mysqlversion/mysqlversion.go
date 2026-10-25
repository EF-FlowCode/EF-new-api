package mysqlversion

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

var mysql8VersionPattern = regexp.MustCompile(`^8\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z][0-9A-Za-z._-]*)?$`)

var unsupportedServerMarkers = []string{
	"alisql",
	"mariadb",
	"oceanbase",
	"percona",
	"polardb",
	"tidb",
	"vitess",
}

var oracleServerCommentMarkers = []string{
	"mysql community server",
	"mysql enterprise server",
}

// Require8Server verifies that db is backed by MySQL 8.x.
func Require8Server(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	var version, versionComment string
	if err := db.QueryRowContext(ctx, "SELECT VERSION(), @@version_comment").Scan(&version, &versionComment); err != nil {
		return fmt.Errorf("query MySQL version: %w", err)
	}
	return requireOracle8(version, versionComment)
}

func requireOracle8(version string, versionComment string) error {
	if err := require8(version, versionComment); err != nil {
		return err
	}
	comment := strings.ToLower(strings.TrimSpace(versionComment))
	for _, marker := range oracleServerCommentMarkers {
		if strings.Contains(comment, marker) {
			return nil
		}
	}
	return fmt.Errorf("unsupported MySQL server identity %q", versionComment)
}

// Require8 validates the value returned by MySQL's VERSION() function.
func Require8(version string) error {
	return require8(version, "")
}

func require8(version string, versionComment string) error {
	version = strings.TrimSpace(version)
	versionComment = strings.TrimSpace(versionComment)
	identity := strings.ToLower(version + " " + versionComment)
	for _, marker := range unsupportedServerMarkers {
		if strings.Contains(identity, marker) {
			return fmt.Errorf("unsupported MySQL-compatible server %q (%s)", version, versionComment)
		}
	}
	if !mysql8VersionPattern.MatchString(version) {
		return fmt.Errorf("unsupported server version %q", version)
	}
	return nil
}
