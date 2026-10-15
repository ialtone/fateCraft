package challenge

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

type SavedVersionInfo struct {
	PackSlug          string `json:"pack_slug"`
	PackName          string `json:"pack_name"`
	Version           string `json:"version"`
	ContentHash       string `json:"content_hash"`
	CurrentVersion    string `json:"current_version,omitempty"`
	NewStartAvailable bool   `json:"new_start_available"`
	RunAllowed        bool   `json:"run_allowed"`
	Reason            string `json:"reason,omitempty"`
}

func (s *Service) SavedVersionInfo(ctx context.Context, conv *storage.Conversation) (*SavedVersionInfo, error) {
	v, err := s.repo.GetVersion(ctx, conv.ChallengeVersionID)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.GetPack(ctx, v.ChallengePackID)
	if err != nil {
		return nil, err
	}
	manifest, err := decodeManifest(v.Manifest)
	if err != nil {
		return nil, err
	}
	result := &SavedVersionInfo{PackSlug: p.Slug, PackName: manifest.Name, Version: v.Version, ContentHash: conv.ChallengeHash, RunAllowed: true}
	if err := s.CheckSavedVersion(ctx, v.ID, conv.ChallengeHash); err != nil {
		result.RunAllowed = false
		result.Reason = err.Error()
	}
	if p.CurrentVersionID != nil {
		current, err := s.repo.GetVersion(ctx, *p.CurrentVersionID)
		if err != nil {
			return nil, err
		}
		result.CurrentVersion = current.Version
		result.NewStartAvailable = p.Status == storage.ChallengePackActive && current.Status == storage.ChallengeVersionPublished && s.CheckSavedVersion(ctx, current.ID, current.ContentHash) == nil
	}
	return result, nil
}

func (s *Service) CheckDeployment(ctx context.Context) ([]string, error) {
	versions, err := s.repo.RequiredRuntimeVersions(ctx)
	if err != nil {
		return nil, err
	}
	problems := []string{}
	for _, v := range versions {
		if err := s.checkDeploymentVersion(ctx, v); err != nil {
			problems = append(problems, fmt.Sprintf("%s/%s: %v", v.ChallengePackID, v.Version, err))
		}
	}
	return problems, nil
}

func (s *Service) checkDeploymentVersion(ctx context.Context, v storage.ChallengeVersion) error {
	runtime, err := s.repo.GetRuntime(ctx, v.ID)
	if err != nil {
		return err
	}
	contract, err := storage.DecodeGameplayRuntime(*runtime)
	if err != nil {
		return err
	}
	return s.registry.ValidateRuntime(contract)
}

func (s *Service) SelectCurrent(ctx context.Context, packID, versionID, actor uuid.UUID, expected int64) error {
	doc, err := s.VersionDocument(ctx, versionID)
	if err != nil {
		return err
	}
	if doc.Version.ChallengePackID != packID {
		return storage.ErrNotFound
	}
	if err := s.ValidateDocument(ctx, doc); err != nil {
		return err
	}
	hash, err := ContentHashDocument(doc)
	if err != nil {
		return err
	}
	if hash != doc.Version.ContentHash {
		return ErrInvalidChallenge
	}
	return s.repo.SelectCurrent(ctx, packID, versionID, actor, expected)
}
