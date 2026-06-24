package keystore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// KeyRecord represents a secure entry within the in-memory key store.
type KeyRecord struct {
	UUID        string
	Alias       string
	Route       string
	RealKey     string
	InternalKey string
	KeyHash     string // SHA-256 hash of InternalKey, used for lookup matching
}

// Store manages API credentials with thread-safe atomic memory access.
//
// Two indexes are maintained over the same underlying records:
//   - keys:   indexed by UUID, used for admin operations (list/delete)
//   - byHash: indexed by KeyHash, used for O(1) lookup on every proxied request
//
// Looking up by hash (rather than scanning and comparing InternalKey directly)
// means the plaintext internal key is never compared in a loop, and the lookup
// path mirrors how a persisted store (e.g. SQLite) would work if added later --
// only the hash would need to be stored there, never the real key.
type Store struct {
	mu     sync.RWMutex
	keys   map[string]*KeyRecord // indexed by UUID
	byHash map[string]*KeyRecord // indexed by KeyHash
}

// NewStore initializes a fresh memory-only secure KeyStore repository.
func NewStore() *Store {
	return &Store{
		keys:   make(map[string]*KeyRecord),
		byHash: make(map[string]*KeyRecord),
	}
}

// hashInternalKey computes the SHA-256 hash of an internal key string.
func hashInternalKey(internalKey string) string {
	hasher := sha256.New()
	hasher.Write([]byte(internalKey))
	return hex.EncodeToString(hasher.Sum(nil))
}

// AddKey generates a unique internal key and records a new upstream credential target.
func (s *Store) AddKey(route, realKey, alias string) (*KeyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Strategic Defense: Scan for duplicate upstream credentials before allocating space
	for _, existingRecord := range s.keys {
		if existingRecord.Route == route && existingRecord.RealKey == realKey {
			return nil, fmt.Errorf("upstream authorization token already exists in mapping pool under alias %q", existingRecord.Alias)
		}
	}

	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("failed to generate secure tracking identity: %w", err)
	}

	// Generate a secure crypto-random token prefixed with "sk-local-"
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("failed to generate entropy token: %w", err)
	}
	internalKey := fmt.Sprintf("sk-local-%s", hex.EncodeToString(bytes))
	keyHash := hashInternalKey(internalKey)

	record := &KeyRecord{
		UUID:        id.String(),
		Alias:       alias,
		Route:       route,
		RealKey:     realKey,
		InternalKey: internalKey,
		KeyHash:     keyHash,
	}

	s.keys[record.UUID] = record
	s.byHash[record.KeyHash] = record
	return record, nil
}

// DeleteKey atomically evicts a credential profiling entity from active routing paths.
func (s *Store) DeleteKey(uid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, exists := s.keys[uid]
	if !exists {
		return false
	}
	delete(s.keys, uid)
	delete(s.byHash, record.KeyHash)
	return true
}

// ListKeys copies internal record arrays safely to avoid execution-time trace race conditions.
func (s *Store) ListKeys() []KeyRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]KeyRecord, 0, len(s.keys))
	for _, record := range s.keys {
		list = append(list, *record)
	}
	return list
}

// GetKeyByInternal performs an O(1) reverse-lookup to match an incoming sk-local
// key against the store. The incoming key is hashed first and compared against
// the byHash index -- the plaintext internal key is never scanned or compared
// directly against stored records.
func (s *Store) GetKeyByInternal(internalKey string) (KeyRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hash := hashInternalKey(internalKey)
	record, ok := s.byHash[hash]
	if !ok {
		return KeyRecord{}, false
	}
	return *record, true
}

// MaskKey is a static formatting helper to securely redact active authorization payloads.
func MaskKey(key string) string {
	if len(key) <= 6 {
		return "******"
	}
	return key[:6] + "******"
}
