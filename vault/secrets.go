package vault

import (
	"errors"
	"sync"
)

// Service is the credential store's service of every key: the app's
// identifier, so that the app and its command line tool, the same signed
// program, share them.
const Service = "dev.horizon.keybar"

// MaxValueSize is the largest value: what the Windows Credential Manager
// keeps (mygo.MaxSecretSize).
const MaxValueSize = 2560

// ErrNotFound is a key without a value in the store.
var ErrNotFound = errors.New("钥匙串里没有这个密钥的值")

// Secrets is the credential store: the system's (mygo.Secrets), or Memory
// in tests and the example data.
type Secrets interface {
	Get(account string) ([]byte, error)
	Set(account string, value []byte) error
	Delete(account string) error
}

// account is the store's account for a key.
func account(name string) string { return "key/" + name }

// Memory is a credential store in memory.
type Memory struct {
	mu   sync.Mutex
	m    map[string][]byte
	Fail error // every call fails with it, when set
}

func (m *Memory) Get(a string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return nil, m.Fail
	}
	v, ok := m.m[a]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), v...), nil
}

func (m *Memory) Set(a string, v []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return m.Fail
	}
	if m.m == nil {
		m.m = map[string][]byte{}
	}
	m.m[a] = append([]byte(nil), v...)
	return nil
}

func (m *Memory) Delete(a string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return m.Fail
	}
	delete(m.m, a)
	return nil
}

// Len is how many values it holds.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.m)
}
