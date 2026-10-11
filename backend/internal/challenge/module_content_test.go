package challenge

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontent"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/contentrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

// Existing regression fixtures exercise the moved module validators through
// these test-only adapters. Production dispatch is Registry.ValidateContent.
func validateDefinitionDocuments(definitions []storage.GameplayDefinition) error {
	for i := range definitions {
		if definitions[i].Tags == nil {
			definitions[i].Tags = []string{}
		}
	}
	runtime := &gameplay.RuntimeContract{Engine: gameplay.EngineRef{Key: cultivation.CampaignEngineKey, Version: cultivation.CampaignEngineVersion}}
	if err := testRegistry().ValidateDefinitions(moduleContent(runtime, &VersionDocument{Definitions: definitions})); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChallenge, err)
	}
	return nil
}

func validateDefinitionShape(definition storage.GameplayDefinition) error {
	content := moduleContent(nil, &VersionDocument{Definitions: []storage.GameplayDefinition{definition}})
	return contentrules.ValidateShape(content.Definitions[0])
}

func validateCampaignDefinitionReferences(runtime *gameplay.RuntimeContract, definitions []storage.GameplayDefinition, requireProgression ...bool) error {
	content := moduleContent(runtime, &VersionDocument{Definitions: definitions})
	var err error
	if len(requireProgression) > 0 && !requireProgression[0] {
		err = worldcontent.ValidateGeneric(content)
	} else {
		err = contentrules.ValidateReferences(runtime, content.Definitions)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChallenge, err)
	}
	return nil
}
