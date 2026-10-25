package common

import "testing"

func TestDeploymentRoles(t *testing.T) {
	oldRole, oldMaster := DeploymentRole, IsMasterNode
	t.Cleanup(func() { DeploymentRole, IsMasterNode = oldRole, oldMaster })
	t.Setenv("SESSION_SECRET", "test-session-secret")
	t.Setenv("REDIS_CONN_STRING", "redis://localhost")
	t.Setenv("NODE_NAME", "test-blue")
	t.Setenv("BATCH_UPDATE_ENABLED", "false")
	for _, role := range []string{"web", "worker", "migrate"} {
		t.Setenv("DEPLOYMENT_ROLE", role)
		if err := InitDeploymentRole(); err != nil {
			t.Fatal(err)
		}
		if IsMasterNode != (role != "web") {
			t.Fatalf("wrong master mode for %s", role)
		}
	}
	t.Setenv("DEPLOYMENT_ROLE", "unknown")
	if InitDeploymentRole() == nil {
		t.Fatal("accepted unknown role")
	}
	t.Setenv("DEPLOYMENT_ROLE", "web")
	t.Setenv("BATCH_UPDATE_ENABLED", "true")
	if InitDeploymentRole() == nil {
		t.Fatal("accepted batched quota writes")
	}
	t.Setenv("BATCH_UPDATE_ENABLED", "false")
	t.Setenv("NODE_NAME", "../shared")
	if InitDeploymentRole() == nil {
		t.Fatal("accepted unsafe cache namespace")
	}
}
