package settings

import (
	"context"

	"github.com/HarikiRito/ai-proxy/backend/internal/ent"
	entsetting "github.com/HarikiRito/ai-proxy/backend/internal/ent/setting"
)

const rowID = 1

type store struct{ db *ent.Client }

func (st *store) get(ctx context.Context) (Settings, error) {
	row, err := st.db.Setting.Get(ctx, rowID)
	if ent.IsNotFound(err) {
		return defaults(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		MaxBodyBytes:           row.MaxBodyBytes,
		RequestTimeoutSeconds:  row.RequestTimeoutSeconds,
		QueueWaitSeconds:       row.QueueWaitSeconds,
		DefaultCooldownSeconds: row.DefaultCooldownSeconds,
	}, nil
}

// save updates the singleton row, creating it on first write.
func (st *store) save(ctx context.Context, s Settings) error {
	n, err := st.db.Setting.Update().Where(entsetting.ID(rowID)).
		SetMaxBodyBytes(s.MaxBodyBytes).
		SetRequestTimeoutSeconds(s.RequestTimeoutSeconds).
		SetQueueWaitSeconds(s.QueueWaitSeconds).
		SetDefaultCooldownSeconds(s.DefaultCooldownSeconds).
		Save(ctx)
	if err != nil || n > 0 {
		return err
	}
	return st.db.Setting.Create().
		SetMaxBodyBytes(s.MaxBodyBytes).
		SetRequestTimeoutSeconds(s.RequestTimeoutSeconds).
		SetQueueWaitSeconds(s.QueueWaitSeconds).
		SetDefaultCooldownSeconds(s.DefaultCooldownSeconds).
		Exec(ctx)
}
