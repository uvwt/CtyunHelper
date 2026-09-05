package storage

import (
	"fmt"

	"github.com/uvwt/CtyunHelper/internal/ctyun/auth"
)

const (
	authProfileFile  = "auth.dat"
	clinkProfileFile = "clink_auth.dat"
)

type AccountStore struct {
	paths Paths
}

func NewAccountStore(paths Paths) *AccountStore {
	return &AccountStore{paths: paths}
}

func (s *AccountStore) SaveAccount(account string) error {
	config, err := LoadConfig(s.paths)
	if err != nil {
		return err
	}
	config.Account = account
	return SaveConfig(s.paths, config)
}

func (s *AccountStore) SaveLogin(account, password string) error {
	return SaveCredential(CredentialTarget, account, password)
}

func (s *AccountStore) LoadLogin() (account, password string, err error) {
	return LoadCredential(CredentialTarget)
}

func (s *AccountStore) DeleteLogin() error {
	return DeleteCredential(CredentialTarget)
}

func (s *AccountStore) SaveProfile(account string, profile auth.Profile) error {
	if account == "" {
		return fmt.Errorf("storage: 保存 Profile 时账号不能为空")
	}
	return SaveProtectedJSON(s.paths, authProfileFile, cachedProfile{Account: account, Profile: profile})
}

func (s *AccountStore) LoadProfile(account string) (auth.Profile, error) {
	var cached cachedProfile
	if err := LoadProtectedJSON(s.paths, authProfileFile, &cached); err != nil {
		return auth.Profile{}, err
	}
	if cached.Account == "" || cached.Account != account {
		return auth.Profile{}, fmt.Errorf("storage: Profile 所属账号与当前账号不一致")
	}
	if cached.Profile.UserID == 0 || cached.Profile.SecretKey == "" || cached.Profile.CommonLoginReqHeader == "" {
		return auth.Profile{}, fmt.Errorf("storage: Profile 缺少必要鉴权字段")
	}
	return cached.Profile, nil
}

func (s *AccountStore) DeleteProfile() error {
	return DeleteProtected(s.paths, authProfileFile)
}

func (s *AccountStore) SaveClinkProfile(account string, profile auth.Profile) error {
	if account == "" {
		return fmt.Errorf("storage: 保存 Clink Profile 时账号不能为空")
	}
	return SaveProtectedJSON(s.paths, clinkProfileFile, cachedProfile{Account: account, Profile: profile})
}

func (s *AccountStore) LoadClinkProfile(account string) (auth.Profile, error) {
	var cached cachedProfile
	if err := LoadProtectedJSON(s.paths, clinkProfileFile, &cached); err != nil {
		return auth.Profile{}, err
	}
	if cached.Account == "" || cached.Account != account {
		return auth.Profile{}, fmt.Errorf("storage: Clink Profile 所属账号与当前账号不一致")
	}
	if cached.Profile.UserID == 0 || cached.Profile.SecretKey == "" || cached.Profile.CommonLoginReqHeader == "" {
		return auth.Profile{}, fmt.Errorf("storage: Clink Profile 缺少必要鉴权字段")
	}
	return cached.Profile, nil
}

func (s *AccountStore) DeleteClinkProfile() error {
	return DeleteProtected(s.paths, clinkProfileFile)
}

type cachedProfile struct {
	Account string       `json:"account"`
	Profile auth.Profile `json:"profile"`
}
