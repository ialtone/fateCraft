package campaigncontract

import "github.com/ialtone/fatecraft/internal/gameplay/components/worldcontract"

const (
	EngineKey               = "cultivation.campaign"
	EngineVersion           = 2
	ActionRenameCharacter   = "character.rename"
	ActionPractice          = "cultivation.practice"
	ActionRetreat           = "cultivation.retreat"
	ActionRecover           = "character.recover"
	ActionBreakthrough      = "cultivation.attempt_breakthrough"
	ActionBeginAscension    = "cultivation.begin_ascension"
	ActionLearnTechnique    = "cultivation.learn_technique"
	ActionEquipTechnique    = "cultivation.equip_technique"
	ActionUnequipTechnique  = "cultivation.unequip_technique"
	ActionApplyCondition    = "character.apply_condition"
	ActionResolveCondition  = "character.resolve_condition"
	ActionAdjustVitals      = "character.adjust_vitals"
	ActionGainItem          = worldcontract.ActionGainItem
	ActionUseItem           = "inventory.use_item"
	ActionGainEquipment     = "inventory.gain_equipment"
	ActionEquipItem         = "inventory.equip_item"
	ActionUnequipItem       = "inventory.unequip_item"
	ActionLearnRecipe       = "craft.learn_recipe"
	ActionCraftRecipe       = "craft.execute_recipe"
	ActionTradeBuy          = worldcontract.ActionTradeBuy
	ActionTradeSell         = worldcontract.ActionTradeSell
	ActionAdjustMarket      = worldcontract.ActionAdjustMarket
	ActionTravel            = worldcontract.ActionTravel
	ActionInvestigate       = worldcontract.ActionInvestigate
	ActionResolveUncertain  = worldcontract.ActionResolveUncertain
	ActionGather            = "world.gather"
	ActionInstantiateNPC    = worldcontract.ActionInstantiateNPC
	ActionMoveNPC           = worldcontract.ActionMoveNPC
	ActionSetNPCStatus      = worldcontract.ActionSetNPCStatus
	ActionRecordInteraction = worldcontract.ActionRecordInteraction
	ActionAdjustFaction     = worldcontract.ActionAdjustFaction
	ActionAcceptQuest       = worldcontract.ActionAcceptQuest
	ActionSubmitQuest       = worldcontract.ActionSubmitQuest
	ActionStartEncounter    = "encounter.start"
	ActionEncounterAct      = "encounter.act"
	ActionEncounterFlee     = "encounter.flee"
	ActionAbandon           = "campaign.abandon"
)

var actionKeys = []string{
	ActionRetreat, ActionRecover,
	ActionBeginAscension,
	ActionRenameCharacter, ActionPractice, ActionBreakthrough, ActionLearnTechnique, ActionEquipTechnique, ActionUnequipTechnique,
	ActionApplyCondition, ActionResolveCondition, ActionAdjustVitals,
	ActionGainItem, ActionUseItem, ActionGainEquipment, ActionEquipItem, ActionUnequipItem,
	ActionLearnRecipe, ActionCraftRecipe, ActionTradeBuy, ActionTradeSell, ActionAdjustMarket,
	ActionTravel, ActionInvestigate, ActionResolveUncertain, ActionGather,
	ActionInstantiateNPC, ActionMoveNPC, ActionSetNPCStatus, ActionRecordInteraction, ActionAdjustFaction,
	ActionAcceptQuest, ActionSubmitQuest, ActionStartEncounter, ActionEncounterAct, ActionEncounterFlee, ActionAbandon,
}

func ActionKeys() []string { return append([]string(nil), actionKeys...) }
