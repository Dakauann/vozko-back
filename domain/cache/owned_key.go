package cache

type OwnedKeyDeleter interface {
	DelIfValue(key, value string) (bool, error)
}
