package service

import (
	"os"
	"slices"
	"testing"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

// scripts/release-pat-scopes.json is the grant release and integration scripts
// give the temporary Root access token they create. It must stay equal to the
// full Root catalog so the scripts keep reaching every console route they use.
func TestReleasePATScopesMatchRootCatalog(t *testing.T) {
	data, err := os.ReadFile("../scripts/release-pat-scopes.json")
	require.NoError(t, err)
	var listed []string
	require.NoError(t, common.Unmarshal(data, &listed))
	var catalog []string
	for _, group := range AccessTokenCatalog(common.RoleRootUser) {
		for _, resource := range group.Resources {
			for _, action := range resource.Actions {
				catalog = append(catalog, resource.Resource+":"+action.Action)
			}
		}
	}
	slices.Sort(catalog)
	require.Equal(t, catalog, listed, "regenerate scripts/release-pat-scopes.json from the Root catalog")
}
