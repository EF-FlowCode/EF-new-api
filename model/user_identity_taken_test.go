package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityTakenCountsEveryMatchingAccount(t *testing.T) {
	truncateTables(t)

	assert.False(t, IsGitHubIdAlreadyTaken("gh-free"))
	require.NoError(t, DB.Create(&User{Username: "gh-one", Password: "password", AffCode: "idt1", GitHubId: "gh-single"}).Error)
	assert.True(t, IsGitHubIdAlreadyTaken("gh-single"))

	// Historical duplicates must stay "taken"; the old RowsAffected == 1 check
	// reported them as free and allowed yet another account to bind.
	require.NoError(t, DB.Create(&User{Username: "wx-one", Password: "password", AffCode: "idt2", WeChatId: "wx-dup"}).Error)
	require.NoError(t, DB.Create(&User{Username: "wx-two", Password: "password", AffCode: "idt3", WeChatId: "wx-dup"}).Error)
	assert.True(t, IsWeChatIdAlreadyTaken("wx-dup"))

	deleted := User{Username: "tg-deleted", Password: "password", AffCode: "idt4", TelegramId: "tg-soft"}
	require.NoError(t, DB.Create(&deleted).Error)
	require.NoError(t, DB.Delete(&deleted).Error)
	assert.True(t, IsTelegramIdAlreadyTaken("tg-soft"), "soft-deleted accounts keep their identity")

	oidcDeleted := User{Username: "oidc-deleted", Password: "password", AffCode: "idt5", OidcId: "oidc-soft"}
	require.NoError(t, DB.Create(&oidcDeleted).Error)
	require.NoError(t, DB.Delete(&oidcDeleted).Error)
	assert.False(t, IsOidcIdAlreadyTaken("oidc-soft"), "OIDC excludes soft-deleted accounts")

	// Empty values are shared by every unbound account and never mean "taken".
	assert.False(t, IsDiscordIdAlreadyTaken(""))
}
