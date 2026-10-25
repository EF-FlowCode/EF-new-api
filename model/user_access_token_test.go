package model

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestAccessToken(t *testing.T, name string) *UserAccessToken {
	t.Helper()
	token := &UserAccessToken{Name: name, TokenHash: AccessTokenFingerprint("nap_" + name), TokenHint: "hint"}
	require.NoError(t, token.SetScopes([]string{"profile:read"}))
	return token
}

func TestUserAccessTokenLimitHoldsUnderConcurrency(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &UserAccessToken{})
	user := User{Username: "pat-limit", Password: "hash", AffCode: "patl01"}
	require.NoError(t, db.Create(&user).Error)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- CreateUserAccessToken(user.Id, newTestAccessToken(t, fmt.Sprintf("limit-%d", i)), 5)
		}(i)
	}
	wg.Wait()
	close(errs)
	var limited int
	for err := range errs {
		if err != nil {
			require.ErrorIs(t, err, ErrAccessTokenLimit)
			limited++
		}
	}
	require.Equal(t, 3, limited)
	tokens, err := ListUserAccessTokens(user.Id)
	require.NoError(t, err)
	require.Len(t, tokens, 5)
	require.Equal(t, []string{"profile:read"}, tokens[0].GetScopes())
}

func TestUserAccessTokenOwnershipAndTouch(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &UserAccessToken{})
	owner := User{Username: "pat-owner", Password: "hash", AffCode: "pato01"}
	other := User{Username: "pat-other", Password: "hash", AffCode: "pato02"}
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&other).Error)
	token := newTestAccessToken(t, "owned")
	require.NoError(t, CreateUserAccessToken(owner.Id, token, 20))

	_, err := GetUserAccessToken(other.Id, token.Id)
	require.ErrorIs(t, err, ErrAccessTokenNotFound)
	_, err = DeleteUserAccessToken(other.Id, token.Id)
	require.ErrorIs(t, err, ErrAccessTokenNotFound)

	updated, err := UpdateUserAccessToken(owner.Id, token.Id, "renamed", nil)
	require.NoError(t, err)
	require.Equal(t, "renamed", updated.Name)
	require.Equal(t, []string{"profile:read"}, updated.GetScopes(), "renaming keeps the grant")

	require.NoError(t, TouchUserAccessToken(token.Id, "203.0.113.9", 1000))
	require.NoError(t, TouchUserAccessToken(token.Id, "203.0.113.10", 1030))
	stored, err := GetUserAccessTokenById(token.Id)
	require.NoError(t, err)
	require.EqualValues(t, 1000, stored.LastUsedAt, "last use is recorded at most once a minute")
	require.Equal(t, "203.0.113.9", stored.LastUsedIp)

	found, err := FindUserAccessTokenByHash(token.TokenHash)
	require.NoError(t, err)
	require.Equal(t, token.Id, found.Id)
	missing, err := FindUserAccessTokenByHash(AccessTokenFingerprint("nap_missing"))
	require.NoError(t, err)
	require.Nil(t, missing)
}

func TestDeletingAccountRemovesAccessTokens(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &UserAccessToken{}, &TwoFA{}, &TwoFABackupCode{}, &PasskeyCredential{}, &Token{}, &UserOAuthBinding{})
	soft := User{Username: "pat-soft", Password: "hash", AffCode: "pats01"}
	hard := User{Username: "pat-hard", Password: "hash", AffCode: "path01"}
	require.NoError(t, db.Create(&soft).Error)
	require.NoError(t, db.Create(&hard).Error)
	require.NoError(t, CreateUserAccessToken(soft.Id, newTestAccessToken(t, "soft"), 20))
	require.NoError(t, CreateUserAccessToken(hard.Id, newTestAccessToken(t, "hard"), 20))

	require.NoError(t, DeleteUserById(soft.Id))
	require.NoError(t, HardDeleteUserById(hard.Id))
	var count int64
	require.NoError(t, db.Model(&UserAccessToken{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestLegacyAccessTokenRetireDeadline(t *testing.T) {
	db := useModelIntegrationTestDB(t, &Option{}, &User{})
	restore := SetLegacyAccessTokenRetireAtForTest(0)
	t.Cleanup(restore)
	require.True(t, LegacyAccessTokensRetired(1), "an unloaded deadline fails closed")

	require.NoError(t, EnsureLegacyAccessTokenRetireAt(1_000))
	want := int64(1_000 + 30*24*60*60)
	require.Equal(t, want, LegacyAccessTokenRetireAt())
	require.NoError(t, EnsureLegacyAccessTokenRetireAt(9_999_999), "later starts never move the deadline")
	require.Equal(t, want, LegacyAccessTokenRetireAt())
	require.False(t, LegacyAccessTokensRetired(want-1))
	require.True(t, LegacyAccessTokensRetired(want))

	var option Option
	require.NoError(t, db.Where("`key` = ?", LegacyAccessTokenRetireAtKey).First(&option).Error)
	require.ErrorIs(t, updateOptionsValidated(map[string]string{LegacyAccessTokenRetireAtKey: "1"}, nil), ErrLegacyRetireAtReadOnly)

	user := User{Username: "pat-legacy", Password: "hash", AffCode: "patg01"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, SetLegacyUserAccessToken(user.Id, "legacy-token-value-0000000000000"))
	ref, err := RevokeLegacyAccessToken(user.Id)
	require.NoError(t, err)
	require.Equal(t, AccessTokenFingerprint("legacy-token-value-0000000000000"), ref)
	ref, err = RevokeLegacyAccessToken(user.Id)
	require.NoError(t, err)
	require.Empty(t, ref)
}

func TestAccessTokenSessionIDRoundTrip(t *testing.T) {
	id, ok := ParseAccessTokenSessionID(AccessTokenSessionID(42))
	require.True(t, ok)
	require.Equal(t, 42, id)
	for _, invalid := range []string{"pat:", "pat:0", "pat:-1", "pat:01", "session:42"} {
		_, ok := ParseAccessTokenSessionID(invalid)
		require.False(t, ok, invalid)
	}
	require.Equal(t, "abcd", AccessTokenHint("nap_xyzabcd"))
}

func TestPasskeyDomainInUseCannotBeRemoved(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &PasskeyCredential{})
	user := User{Username: "rp-user", Password: "hash", AffCode: "rpus01"}
	require.NoError(t, db.Create(&user).Error)
	rpID := "old.example.org"
	require.NoError(t, db.Create(&PasskeyCredential{UserID: user.Id, CredentialID: "rp-cred", PublicKey: "pk", RPID: &rpID}).Error)

	require.NoError(t, validatePasskeyRPIDChange("e-flowcode.cc, old.example.org"))
	err := validatePasskeyRPIDChange("e-flowcode.cc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "old.example.org")
	require.NoError(t, validatePasskeyRPIDChange(""), "clearing the setting falls back to the server address")
}
