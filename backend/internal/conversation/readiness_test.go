package conversation

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llmsettings"
)

type unconfiguredRoute struct{}

func (unconfiguredRoute) ReadyRoute(context.Context, string) (string, error) {
	return "", llmsettings.ErrConfigurationIncomplete
}

func TestCreateRejectsMissingModelConfigurationBeforeCreatingState(t *testing.T) {
	ctx := identity.WithPlayerID(context.Background(), uuid.NewString())
	// No repositories are provided: missing configuration must fail before any writes or content loading.
	service := NewService(nil, nil, nil, nil, identity.NewContext())
	service.SetRouteResolver(unconfiguredRoute{})
	if _, err := service.Create(ctx, CreateInput{ChallengeSlug: "test"}); !errors.Is(err, llmsettings.ErrConfigurationIncomplete) {
		t.Fatalf("Create error = %v", err)
	}
}
