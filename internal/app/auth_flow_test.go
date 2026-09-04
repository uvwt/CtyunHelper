package app

import (
	"errors"
	"os"
	"testing"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

type memoryAccountStore struct {
	account       string
	password      string
	profile       auth.Profile
	profileExists bool
	deleteProfile int
}

func (s *memoryAccountStore) SaveAccount(account string) error {
	s.account = account
	return nil
}
func (s *memoryAccountStore) SaveLogin(account, password string) error {
	s.account, s.password = account, password
	return nil
}
func (s *memoryAccountStore) LoadLogin() (string, string, error) {
	if s.account == "" {
		return "", "", os.ErrNotExist
	}
	return s.account, s.password, nil
}
func (s *memoryAccountStore) DeleteLogin() error {
	s.account, s.password = "", ""
	return nil
}
func (s *memoryAccountStore) SaveProfile(_ string, profile auth.Profile) error {
	s.profile, s.profileExists = profile, true
	return nil
}
func (s *memoryAccountStore) LoadProfile(string) (auth.Profile, error) {
	if !s.profileExists {
		return auth.Profile{}, os.ErrNotExist
	}
	return s.profile, nil
}
func (s *memoryAccountStore) DeleteProfile() error {
	s.profileExists = false
	s.deleteProfile++
	return nil
}

func TestAuthFlowRestoresCachedProfileWithoutRelogin(t *testing.T) {
	profile := auth.Profile{UserID: 123, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	model := NewModel(State{})
	flow := NewAuthFlow(client, store, model)
	restored, err := flow.Restore("account")
	if err != nil || !restored {
		t.Fatalf("Restore() restored=%v err=%v", restored, err)
	}
	got, ok := client.Profile()
	if !ok || got.UserID != 123 {
		t.Fatalf("client profile = %#v, ok=%v", got, ok)
	}
	if model.Snapshot().Connection != ConnectionStopped {
		t.Fatalf("state = %#v", model.Snapshot())
	}
}

func TestAuthFlowOnlyClearsProfileOnExplicitAuthFailure(t *testing.T) {
	profile := auth.Profile{UserID: 123, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: true}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	client.UseProfile(profile)
	model := NewModel(State{Account: "account"})
	flow := NewAuthFlow(client, store, model)

	if err := flow.HandleSessionError(errors.New("network down")); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.Profile(); !ok || store.deleteProfile != 0 {
		t.Fatal("network error must not clear profile")
	}

	if err := flow.HandleSessionError(auth.APIError{Code: auth.CodeNoPermissions, Message: "expired"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.Profile(); ok || store.profileExists || store.deleteProfile != 1 {
		t.Fatal("auth failure must clear cached profile")
	}
	if model.Snapshot().Connection != ConnectionAuth {
		t.Fatalf("state = %#v", model.Snapshot())
	}
}

func TestAuthFlowKeepsUnboundProfileForBinding(t *testing.T) {
	profile := auth.Profile{UserID: 123, SecretKey: "test", CommonLoginReqHeader: "common", BondedDevice: false}
	store := &memoryAccountStore{profile: profile, profileExists: true}
	client := auth.NewClient(auth.DeviceIdentity{Code: "ctyun_fixed"}, auth.ClientOptions{})
	model := NewModel(State{})
	flow := NewAuthFlow(client, store, model)
	if _, err := flow.Restore("account"); err != nil {
		t.Fatal(err)
	}
	if model.Snapshot().Connection != ConnectionDeviceBind {
		t.Fatalf("state = %#v", model.Snapshot())
	}
	if _, ok := client.Profile(); !ok {
		t.Fatal("unbound profile must remain available for binding")
	}
}
