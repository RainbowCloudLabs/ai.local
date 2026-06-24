package keystore

import (
	"strings"
	"testing"
)

// 1. Add one key and verify containment via ListKeys
func TestStore_AddOneKey(t *testing.T) {
	store := NewStore()

	// Add the first key
	rec, err := store.AddKey("/openrouter_small", "sk-or-real-key-1111", "test-alias-1")
	if err != nil {
		t.Fatalf("unexpected error adding key: %v", err)
	}

	// Verify via ListKeys
	keys := store.ListKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key inside store, got %d", len(keys))
	}

	// Verify structural integrity of the returned record against the listed one
	if keys[0].UUID != rec.UUID || keys[0].RealKey != "sk-or-real-key-1111" || keys[0].Alias != "test-alias-1" {
		t.Errorf("mismatch in stored key details: %+v", keys[0])
	}
}

// 2. Add two keys and verify both exist via ListKeys
func TestStore_AddTwoKeys(t *testing.T) {
	store := NewStore()

	// Add two distinct routing keys
	_, _ = store.AddKey("/openrouter_small", "sk-or-real-key-1111", "test-alias-1")
	_, _ = store.AddKey("/openrouter_unlimit", "sk-or-real-key-2222", "test-alias-2")

	// Verify volume depth via ListKeys
	keys := store.ListKeys()
	if len(keys) != 2 {
		t.Fatalf("expected exactly 2 keys inside store, got %d", len(keys))
	}

	// Cross-reference routing paths to ensure data didn't corrupt or overwrite
	foundSmall := false
	foundUnlimit := false
	for _, k := range keys {
		if k.Route == "/openrouter_small" && k.RealKey == "sk-or-real-key-1111" {
			foundSmall = true
		}
		if k.Route == "/openrouter_unlimit" && k.RealKey == "sk-or-real-key-2222" {
			foundUnlimit = true
		}
	}

	if !foundSmall || !foundUnlimit {
		t.Error("keystore failed to preserve distinct routing targets for multiple keys")
	}
}

// 3. Add two keys, delete one key, and verify only the remaining one exists via ListKeys
func TestStore_AddTwoKeysAndDeleteOne(t *testing.T) {
	store := NewStore()

	// Add two keys, capture the first one's UUID for eviction
	rec1, _ := store.AddKey("/openrouter_small", "sk-or-real-key-1111", "test-alias-1")
	_, _ = store.AddKey("/openrouter_unlimit", "sk-or-real-key-2222", "test-alias-2")

	// Verify initial deployment depth
	initialKeys := store.ListKeys()
	if len(initialKeys) != 2 {
		t.Fatalf("pre-delete state corrupted: expected 2 keys, got %d", len(initialKeys))
	}

	// Atomic destruction of the first key
	success := store.DeleteKey(rec1.UUID)
	if !success {
		t.Fatalf("failed to delete active tracking target UUID: %s", rec1.UUID)
	}

	// Post-destruction check via ListKeys
	postKeys := store.ListKeys()
	if len(postKeys) != 1 {
		t.Fatalf("expected exactly 1 key remaining after deletion, got %d", len(postKeys))
	}

	// Ensure the remaining key is strictly the second one
	if postKeys[0].UUID == rec1.UUID || postKeys[0].Route != "/openrouter_unlimit" {
		t.Errorf("wrong key evicted from active routing paths, remaining: %+v", postKeys[0])
	}
}

// 4. Delete a non-existent key and verify the store configuration remains untouched via ListKeys
func TestStore_DeleteNonExistentKey(t *testing.T) {
	store := NewStore()

	// Seed the database with 1 healthy baseline key
	_, _ = store.AddKey("/openrouter_small", "sk-or-real-key-1111", "test-alias-1")

	// Attempt to evict a ghost fake UUID
	fakeUUID := "00000000-0000-0000-0000-000000000000"
	success := store.DeleteKey(fakeUUID)
	if success {
		t.Error("store reported successful deletion for a non-existent tracking identity")
	}

	// Verify that the operation did not corrupt or flush existing entries via ListKeys
	keys := store.ListKeys()
	if len(keys) != 1 {
		t.Fatalf("keystore volume depth corrupted after ghost deletion: expected 1, got %d", len(keys))
	}

	if keys[0].Route != "/openrouter_small" {
		t.Errorf("baseline key was unexpectedly mutated or destroyed: %+v", keys[0])
	}
}

// 5. GetKeyByInternal finds a key by its plaintext internal key (matched via hash).
func TestStore_GetKeyByInternal_Found(t *testing.T) {
	store := NewStore()

	rec, err := store.AddKey("/claude", "sk-ant-real-1111", "alice")
	if err != nil {
		t.Fatalf("unexpected error adding key: %v", err)
	}

	got, ok := store.GetKeyByInternal(rec.InternalKey)
	if !ok {
		t.Fatal("expected to find key by internal key, got not found")
	}
	if got.UUID != rec.UUID {
		t.Errorf("UUID = %q, want %q", got.UUID, rec.UUID)
	}
	if got.RealKey != "sk-ant-real-1111" {
		t.Errorf("RealKey = %q, want %q", got.RealKey, "sk-ant-real-1111")
	}
}

// 6. GetKeyByInternal returns false for an unknown internal key.
func TestStore_GetKeyByInternal_NotFound(t *testing.T) {
	store := NewStore()
	_, _ = store.AddKey("/claude", "sk-ant-real-1111", "alice")

	_, ok := store.GetKeyByInternal("sk-local-doesnotexist")
	if ok {
		t.Error("expected not found for unknown internal key")
	}
}

//  7. After DeleteKey, GetKeyByInternal must no longer find the deleted key
//     (verifies byHash index is cleaned up alongside the UUID index).
func TestStore_GetKeyByInternal_AfterDelete(t *testing.T) {
	store := NewStore()
	rec, _ := store.AddKey("/claude", "sk-ant-real-1111", "alice")

	store.DeleteKey(rec.UUID)

	_, ok := store.GetKeyByInternal(rec.InternalKey)
	if ok {
		t.Error("expected key to be unreachable via GetKeyByInternal after deletion")
	}
}

// 8. With multiple keys, GetKeyByInternal must return the correct one, not just any match.
func TestStore_GetKeyByInternal_MultipleKeys(t *testing.T) {
	store := NewStore()
	rec1, _ := store.AddKey("/claude", "sk-ant-real-1111", "alice")
	rec2, _ := store.AddKey("/openrouter", "sk-or-real-2222", "bob")

	got1, ok := store.GetKeyByInternal(rec1.InternalKey)
	if !ok || got1.UUID != rec1.UUID {
		t.Errorf("lookup for rec1 returned wrong record: %+v", got1)
	}

	got2, ok := store.GetKeyByInternal(rec2.InternalKey)
	if !ok || got2.UUID != rec2.UUID {
		t.Errorf("lookup for rec2 returned wrong record: %+v", got2)
	}
}

// 9. Add duplicated realKey.
func TestStore_AddDuplicatedRealKey(t *testing.T) {
	store := NewStore()
	rec1, _ := store.AddKey("/claude", "sk-ant-real-1111", "alice")
	got1, ok := store.GetKeyByInternal(rec1.InternalKey)
	if !ok || got1.UUID != rec1.UUID {
		t.Errorf("lookup for rec1 returned wrong record: %+v", got1)
	}

	_, err := store.AddKey("/claude", "sk-ant-real-1111", "bob")
	if err == nil {
		t.Fatal("expected error for duplicated real key on the same route, got nil")
	}
	if !strings.Contains(err.Error(), "upstream authorization token already exists") {
		t.Errorf("unexpected error message: %v", err)
	}
}
