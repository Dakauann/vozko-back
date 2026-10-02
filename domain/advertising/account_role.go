package advertising

import (
	"slices"
	"strings"
)

type AccountRole string

const (
	RoleAdmin      AccountRole = "admin"
	RoleAdvertiser AccountRole = "advertiser"
	RoleReadOnly   AccountRole = "read_only"
)

const (
	TaskManage    = "MANAGE"
	TaskAdvertise = "ADVERTISE"
)

func hasTask(tasks []string, want string) bool {
	return slices.ContainsFunc(tasks, func(task string) bool { return strings.EqualFold(strings.TrimSpace(task), want) })
}

func RoleFromTasks(tasks []string) AccountRole {
	switch {
	case hasTask(tasks, TaskManage):
		return RoleAdmin
	case hasTask(tasks, TaskAdvertise):
		return RoleAdvertiser
	}
	return RoleReadOnly
}

func TasksAllowAdvertising(tasks []string) bool { return RoleFromTasks(tasks) != RoleReadOnly }

type AccountUse int

const (
	UseRead AccountUse = iota + 1
	UseWrite
	UseBilling
)

func (u AccountUse) Scope() string {
	if u == UseRead {
		return ScopeAdsRead
	}
	return ScopeAdsManagement
}

func (a *AdAccount) Role() AccountRole { return RoleFromTasks(a.Tasks) }

func (a *AdAccount) Allows(use AccountUse) error {
	switch use {
	case UseRead:
		return a.CanRead()
	case UseBilling:
		return a.CanChangeBilling()
	}
	return a.CanManage()
}

func (a *AdAccount) CanRead() error {
	if a == nil {
		return ErrAccountNotFound
	}
	if a.Connection != ConnectionConnected {
		return ErrAccountNeedsReconnect
	}
	return nil
}

func (a *AdAccount) CanManage() error {
	if err := a.CanRead(); err != nil {
		return err
	}
	return a.checkRole()
}

func (a *AdAccount) checkRole() error {
	if a.Role() == RoleReadOnly {
		return ErrAccountReadOnly
	}
	return nil
}

func (a *AdAccount) CanChangeBilling() error {
	if err := a.CanManage(); err != nil {
		return err
	}
	if a.Role() != RoleAdmin {
		return ErrAccountAdminRequired
	}
	return nil
}
