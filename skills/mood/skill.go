package mood

import (
	"context"
	"encoding/json"
	"strings"

	"yascore/config"
)

// Skill answers the mood tool and remembers one mood.
type Skill struct {
	store *Store
}

// New opens the mood document. Nothing is written until the first switch.
func New(s Settings) (*Skill, error) {
	s = s.normalized()
	store, err := OpenStore(s.Database)
	if err != nil {
		return nil, err
	}
	return &Skill{store: store}, nil
}

// Close releases the store.
func (s *Skill) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

// Handle is the protocol callback. Arguments are the tool JSON.
func (s *Skill) Handle(ctx context.Context, _ string, arguments string) (string, error) {
	var args struct {
		Mood string `json:"mood"`
	}
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return failPhrase, nil
		}
	}
	return s.Ask(ctx, args.Mood)
}

// Ask switches the mood or reports the current one.
// An empty mood, «какое настроение» and «какая ты сейчас» speak the current mood and do not write.
// An unknown name speaks the known ones and leaves the saved choice as it was.
func (s *Skill) Ask(ctx context.Context, raw string) (string, error) {
	saved, ok, err := s.store.Load(ctx)
	if err != nil {
		config.Error("mood", "err", err)
		return failPhrase, nil
	}
	m, kind := kindOf(raw)
	switch kind {
	case kindQuery:
		return currentPhrase(saved, ok), nil
	case kindUnknown:
		return knownPhrase(), nil
	}
	if err := s.store.Save(ctx, m.id); err != nil {
		config.Error("mood", "err", err)
		return failPhrase, nil
	}
	return m.confirm(), nil
}

func currentPhrase(id string, ok bool) string {
	if !ok {
		return nonePhrase
	}
	if m, found := byKey[fold(id)]; found {
		return m.current()
	}
	return nonePhrase
}
