package campaigninit

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldinit"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/originrules"
	"github.com/ialtone/fatecraft/internal/storage"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strings"
	"time"
)

func Initialize(tx *gorm.DB, campaign *storage.Campaign) error {
	var definitions []storage.GameplayDefinition
	if err := tx.Where("challenge_version_id = ?", campaign.ChallengeVersionID).
		Order("kind ASC, sort_order ASC, key ASC").Find(&definitions).Error; err != nil {
		return err
	}
	initialRealm, initialRealmRank, err := selectInitialCampaignRealm(definitions)
	if err != nil {
		return err
	}
	startingTechniques, err := StartingTechniqueKeys(definitions, campaign.RandomSeed)
	if err != nil {
		return err
	}
	name := "无名修士"
	personaKey := ""
	if campaign.PersonaAssetID != nil {
		var asset storage.ChallengeAsset
		if err := tx.First(&asset, "id = ? AND challenge_version_id = ?", *campaign.PersonaAssetID, campaign.ChallengeVersionID).Error; err != nil {
			return err
		} else {
			personaKey = asset.Slug
			var identity struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(asset.Content, &identity) == nil && strings.TrimSpace(identity.Name) != "" {
				name = identity.Name
			}
		}
	}
	origin, err := selectOrigin(definitions, personaKey)
	if err != nil {
		return err
	}
	now := time.Now()
	var character *storage.CampaignCharacter
	if initialRealm != nil {
		stages, _ := initialRealm.Content["stages"].([]any)
		stageKey := "initial"
		if len(stages) > 0 {
			stageKey, _ = stages[0].(string)
		}
		required, _ := worldinit.Int(initialRealm.Content["cultivation_required"])
		lifespan, _ := worldinit.Int(initialRealm.Content["lifespan_days"])
		maxHealth := worldinit.IntDefault(initialRealm.Content["max_health"], 100)
		maxMana := worldinit.IntDefault(initialRealm.Content["max_mana"], 30)
		maxSense := worldinit.IntDefault(initialRealm.Content["max_spiritual_sense"], 20)
		character = &storage.CampaignCharacter{
			ID: uuid.New(), CampaignID: campaign.ID, Name: name,
			RealmKey: initialRealm.Key, StageKey: stageKey, RealmRank: int(initialRealmRank),
			CultivationRequired: required, FoundationQuality: 50,
			AgeDays:      worldinit.IntDefault(initialRealm.Content["starting_age_days"], 16*365),
			LifespanDays: lifespan,
			Health:       maxHealth, BaseMaxHealth: maxHealth, MaxHealth: maxHealth,
			Mana: maxMana, BaseMaxMana: maxMana, MaxMana: maxMana,
			SpiritualSense: maxSense, BaseMaxSpiritualSense: maxSense, MaxSpiritualSense: maxSense,
			Injuries: storage.JSONMap{}, LongTermEffects: storage.JSONMap{}, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(character).Error; err != nil {
			return err
		}
	}
	startingEffects := map[string]int64{}
	for _, definition := range definitions {
		switch definition.Kind {
		case storage.DefinitionTechnique:
			if startingTechniques[definition.Key] {
				category, _ := definition.Content["category"].(string)
				source := storage.JSONMap{"name": definition.Name, "key": definition.Key}
				if pool, _ := definition.Content["starting_pool"].(string); pool != "" {
					source["starting_pool"] = pool
				}
				row := storage.CampaignTechnique{
					ID: uuid.New(), CampaignID: campaign.ID, TechniqueKey: definition.Key,
					Category: category, LearnedLayer: 1, Status: "active",
					SourceSnapshot: source, CreatedAt: now, UpdatedAt: now,
				}
				if slot, _ := definition.Content["starting_slot"].(string); slot != "" {
					row.EquippedSlot = &slot
					effects, ok := effectrules.MasteredEffects(definition.Content, row.Mastery, row.LearnedLayer)
					if !ok {
						return fmt.Errorf("初始功法层级或效果无效: %s", definition.Key)
					}
					effectrules.Add(startingEffects, effects)
				}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
		}
	}
	if character != nil {
		character.MaxHealth = positiveStat(character.BaseMaxHealth + startingEffects["max_health"])
		character.MaxMana = positiveStat(character.BaseMaxMana + startingEffects["max_mana"])
		character.MaxSpiritualSense = positiveStat(character.BaseMaxSpiritualSense + startingEffects["max_spiritual_sense"])
		character.Health = character.MaxHealth
		character.Mana = character.MaxMana
		character.SpiritualSense = character.MaxSpiritualSense
		character.UpdatedAt = now
		if err := tx.Save(character).Error; err != nil {
			return err
		}
	}
	if err := worldinit.Initialize(tx, campaign); err != nil {
		return err
	}
	if origin != nil {
		keys := make([]string, 0, len(origin.Items))
		for key := range origin.Items {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			row := storage.CampaignItemStack{CampaignID: campaign.ID, ItemKey: key, QualityKey: "common", Quantity: origin.Items[key], Metadata: storage.JSONMap{"origin": origin.PersonaKey}, UpdatedAt: now}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "campaign_id"}, {Name: "item_key"}, {Name: "quality_key"}}, DoUpdates: clause.Assignments(map[string]any{"quantity": gorm.Expr("campaign_item_stacks.quantity + EXCLUDED.quantity"), "metadata": gorm.Expr("campaign_item_stacks.metadata || EXCLUDED.metadata"), "updated_at": now})}).Create(&row).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func selectOrigin(definitions []storage.GameplayDefinition, personaKey string) (*originrules.Origin, error) {
	var result *originrules.Origin
	count := 0
	for _, d := range definitions {
		if d.Kind != originrules.DefinitionKind {
			continue
		}
		count++
		if count > 1 {
			return nil, fmt.Errorf("出身配置不能重复")
		}
		c, err := originrules.Parse(d.Content)
		if err != nil {
			return nil, err
		}
		for _, o := range c.Origins {
			if o.PersonaKey == personaKey {
				copy := o
				result = &copy
			}
		}
	}
	if count > 0 && result == nil {
		return nil, fmt.Errorf("所选出身缺少初始化方案: %s", personaKey)
	}
	return result, nil
}

type startingTechniqueCandidate struct {
	key    string
	weight int64
}

// StartingTechniqueKeys keeps explicit starters and deterministically picks
// one candidate from every content-defined pool using the campaign seed.
func StartingTechniqueKeys(definitions []storage.GameplayDefinition, seed int64) (map[string]bool, error) {
	selected := map[string]bool{}
	pools := map[string][]startingTechniqueCandidate{}
	for _, definition := range definitions {
		if definition.Kind != storage.DefinitionTechnique {
			continue
		}
		starting, _ := definition.Content["starting_known"].(bool)
		pool, _ := definition.Content["starting_pool"].(string)
		if starting && pool != "" {
			return nil, fmt.Errorf("功法 %s 不能同时声明 starting_known 与 starting_pool", definition.Key)
		}
		if starting {
			selected[definition.Key] = true
		}
		if pool == "" {
			continue
		}
		if !contract.ValidStableKey(pool) {
			return nil, fmt.Errorf("功法 %s 的 starting_pool 无效", definition.Key)
		}
		weight := worldinit.IntDefault(definition.Content["starting_weight"], 1)
		if weight < 1 || weight > 100000 {
			return nil, fmt.Errorf("功法 %s 的 starting_weight 无效", definition.Key)
		}
		pools[pool] = append(pools[pool], startingTechniqueCandidate{key: definition.Key, weight: weight})
	}

	poolKeys := make([]string, 0, len(pools))
	for pool := range pools {
		poolKeys = append(poolKeys, pool)
	}
	sort.Strings(poolKeys)
	for _, pool := range poolKeys {
		candidates := pools[pool]
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].key < candidates[j].key })
		var total int64
		for _, candidate := range candidates {
			total += candidate.weight
		}
		roll := startingPoolRoll(seed, pool, total)
		for _, candidate := range candidates {
			if roll < candidate.weight {
				selected[candidate.key] = true
				break
			}
			roll -= candidate.weight
		}
	}
	return selected, nil
}

func startingPoolRoll(seed int64, pool string, total int64) int64 {
	var seedBytes [8]byte
	binary.BigEndian.PutUint64(seedBytes[:], uint64(seed))
	hash := sha256.New()
	_, _ = hash.Write(seedBytes[:])
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(pool))
	sum := hash.Sum(nil)
	return int64(binary.BigEndian.Uint64(sum[:8]) % uint64(total))
}

func positiveStat(value int64) int64 {
	if value < 1 {
		return 1
	}
	return value
}

func selectInitialCampaignRealm(definitions []storage.GameplayDefinition) (*storage.GameplayDefinition, int64, error) {
	var initial *storage.GameplayDefinition
	var initialRank int64
	seenRanks := map[int64]string{}
	for index := range definitions {
		definition := &definitions[index]
		if definition.Kind != storage.DefinitionRealm {
			continue
		}
		rank, ok := worldinit.Int(definition.Content["rank"])
		if !ok || rank < 0 || rank > 1000 {
			return nil, 0, fmt.Errorf("storage.Campaign 境界定义 %s 的 rank 无效", definition.Key)
		}
		if previous, exists := seenRanks[rank]; exists {
			return nil, 0, fmt.Errorf("storage.Campaign 境界定义 %s 与 %s 使用了相同 rank %d", definition.Key, previous, rank)
		}
		seenRanks[rank] = definition.Key
		if initial == nil || rank < initialRank {
			initial, initialRank = definition, rank
		}
	}
	if initial == nil {
		return nil, 0, fmt.Errorf("storage.Campaign 缺少境界定义")
	}
	return initial, initialRank, nil
}
