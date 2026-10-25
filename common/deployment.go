package common

import (
	"fmt"
	"os"
	"regexp"
)

var DeploymentRole string

func InitDeploymentRole() error {
	DeploymentRole = os.Getenv("DEPLOYMENT_ROLE")
	switch DeploymentRole {
	case "":
		return nil
	case "web", "worker", "migrate":
	default:
		return fmt.Errorf("invalid DEPLOYMENT_ROLE")
	}
	if os.Getenv("SESSION_SECRET") == "" || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`).MatchString(os.Getenv("NODE_NAME")) {
		return fmt.Errorf("deployment roles require SESSION_SECRET and a unique NODE_NAME")
	}
	if DeploymentRole != "migrate" && os.Getenv("REDIS_CONN_STRING") == "" {
		return fmt.Errorf("rolling instances require shared Redis")
	}
	if os.Getenv("BATCH_UPDATE_ENABLED") == "true" {
		return fmt.Errorf("rolling deployment does not support batch quota updates")
	}
	IsMasterNode = DeploymentRole != "web"
	return nil
}
