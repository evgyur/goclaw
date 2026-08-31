package skills

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type dependencyResetStore struct {
	mu      sync.Mutex
	missing []string
	updates map[string]interface{}
	done    chan struct{}
	once    sync.Once
}

func (s *dependencyResetStore) UpsertSystemSkill(context.Context, store.SkillCreateParams) (uuid.UUID, bool, string, error) {
	return uuid.Nil, false, "", nil
}
func (s *dependencyResetStore) GetNextVersion(context.Context, string) int { return 1 }
func (s *dependencyResetStore) BumpVersion() {
	s.once.Do(func() { close(s.done) })
}
func (s *dependencyResetStore) UpdateSkill(_ context.Context, _ uuid.UUID, updates map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates = updates
	return nil
}
func (s *dependencyResetStore) StoreMissingDeps(_ context.Context, _ uuid.UUID, missing []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.missing = append([]string(nil), missing...)
	return nil
}

func TestCheckDepsAsyncClearsStaleMissingDepsForDependencyFreeProjection(t *testing.T) {
	dir := t.TempDir()
	storeStub := &dependencyResetStore{
		missing: []string{"python3"},
		done:    make(chan struct{}),
	}
	seeder := NewSeeder(t.TempDir(), t.TempDir(), storeStub)
	seeder.CheckDepsAsync([]seededSkill{{id: uuid.New(), slug: "trader20", baseDir: dir}}, nil)

	select {
	case <-storeStub.done:
	case <-time.After(2 * time.Second):
		t.Fatal("dependency reset did not complete")
	}

	storeStub.mu.Lock()
	defer storeStub.mu.Unlock()
	if len(storeStub.missing) != 0 {
		t.Fatalf("missing deps = %v, want empty", storeStub.missing)
	}
	if got := storeStub.updates["status"]; got != "active" {
		t.Fatalf("status update = %v, want active", got)
	}
}
