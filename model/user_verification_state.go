package model

import "errors"

// UserVerificationState summarizes which step-up factors a user has enrolled.
// Mirrors upstream model.UserVerificationState minus the auth version, which
// eflow does not track.
type UserVerificationState struct {
	Role        int
	Status      int
	HasPassword bool
	HasTwoFA    bool
	TwoFALocked bool
	HasPasskey  bool
}

func GetUserVerificationState(userID int) (*UserVerificationState, error) {
	user, err := GetUserById(userID, true)
	if err != nil {
		return nil, err
	}
	state := &UserVerificationState{Role: user.Role, Status: user.Status, HasPassword: user.Password != ""}
	twoFA, err := GetTwoFAByUserId(userID)
	if err != nil {
		return nil, err
	}
	if twoFA != nil && twoFA.IsEnabled {
		state.HasTwoFA = true
		state.TwoFALocked = twoFA.IsLocked()
	}
	if _, err := GetPasskeyByUserID(userID); err == nil {
		state.HasPasskey = true
	} else if !errors.Is(err, ErrPasskeyNotFound) {
		return nil, err
	}
	return state, nil
}

// ErrLastLoginMethod refuses to remove the only way an account can sign in.
var ErrLastLoginMethod = errors.New("This is the account's only sign-in method. Add another one before removing it.")

// OtherSignInMethodCount counts the ways a user can still sign in when the
// custom OAuth binding providerID (0 for none) is removed.
func OtherSignInMethodCount(userID, providerID int) (int, error) {
	user, err := GetUserById(userID, true)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, value := range []string{user.Password, user.GitHubId, user.DiscordId, user.OidcId, user.LinuxDOId, user.WeChatId, user.TelegramId} {
		if value != "" {
			count++
		}
	}
	if _, err := GetPasskeyByUserID(userID); err == nil {
		count++
	} else if !errors.Is(err, ErrPasskeyNotFound) {
		return 0, err
	}
	bindings, err := GetUserOAuthBindingsByUserId(userID)
	if err != nil {
		return 0, err
	}
	for _, binding := range bindings {
		if binding.ProviderId != providerID {
			count++
		}
	}
	return count, nil
}
