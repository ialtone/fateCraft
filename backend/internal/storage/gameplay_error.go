package storage

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
)

var ErrGameplayModuleUnavailable = errors.New("玩法模块或运行契约不可用")

func GameplayPolicyError(err error) error {
	message := ""
	var pg *pgconn.PgError
	var legacy *pq.Error
	if errors.As(err, &pg) {
		message = pg.Message
	} else if errors.As(err, &legacy) {
		message = legacy.Message
	}
	switch message {
	case "FC_VERSION_BLOCKED":
		return ErrVersionBlocked
	case "FC_GAMEPLAY_ENTRY_CHANGED":
		return ErrRevisionConflict
	case "FC_GAMEPLAY_BINDING_INVALID":
		return ErrPublishedVersion
	}
	return err
}
