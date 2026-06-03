package kvdb

// Store defines the persistence interface
type Store interface {
	GetFile(filePath string) ([]byte, error)
	SetFile(filePath string, data []byte) error
	AddToFile(filePath string, data []byte) error
}

// KVStore defines the minimum API
type KVStore interface {
	Get(key string) (string, error)
	Set(key, value string) error
}
