package campaigncontract

const (
	ActionRenameCharacter   = "character.rename"
	ActionPractice          = "cultivation.practice"
	ActionBreakthrough      = "cultivation.attempt_breakthrough"
	ActionLearnTechnique    = "cultivation.learn_technique"
	ActionEquipTechnique    = "cultivation.equip_technique"
	ActionUnequipTechnique  = "cultivation.unequip_technique"
	ActionApplyCondition    = "character.apply_condition"
	ActionResolveCondition  = "character.resolve_condition"
	ActionAdjustVitals      = "character.adjust_vitals"
	ActionGainItem          = "inventory.gain_item"
	ActionUseItem           = "inventory.use_item"
	ActionGainEquipment     = "inventory.gain_equipment"
	ActionEquipItem         = "inventory.equip_item"
	ActionUnequipItem       = "inventory.unequip_item"
	ActionLearnRecipe       = "craft.learn_recipe"
	ActionCraftRecipe       = "craft.execute_recipe"
	ActionTradeBuy          = "trade.buy"
	ActionTradeSell         = "trade.sell"
	ActionAdjustMarket      = "market.adjust_price_modifier"
	ActionTravel            = "travel.move"
	ActionInvestigate       = "world.investigate"
	ActionResolveUncertain  = "fortune.resolve_uncertain"
	ActionGather            = "world.gather"
	ActionInstantiateNPC    = "npc.instantiate_archetype"
	ActionMoveNPC           = "npc.move"
	ActionSetNPCStatus      = "npc.set_status"
	ActionRecordInteraction = "relationship.record_interaction"
	ActionAdjustFaction     = "faction.adjust_standing"
	ActionAcceptQuest       = "quest.accept"
	ActionSubmitQuest       = "quest.submit"
	ActionStartEncounter    = "encounter.start"
	ActionEncounterAct      = "encounter.act"
	ActionEncounterFlee     = "encounter.flee"
)

var actionKeys = []string{
	ActionRenameCharacter, ActionPractice, ActionBreakthrough, ActionLearnTechnique, ActionEquipTechnique, ActionUnequipTechnique,
	ActionApplyCondition, ActionResolveCondition, ActionAdjustVitals,
	ActionGainItem, ActionUseItem, ActionGainEquipment, ActionEquipItem, ActionUnequipItem,
	ActionLearnRecipe, ActionCraftRecipe, ActionTradeBuy, ActionTradeSell, ActionAdjustMarket,
	ActionTravel, ActionInvestigate, ActionResolveUncertain, ActionGather,
	ActionInstantiateNPC, ActionMoveNPC, ActionSetNPCStatus, ActionRecordInteraction, ActionAdjustFaction,
	ActionAcceptQuest, ActionSubmitQuest, ActionStartEncounter, ActionEncounterAct, ActionEncounterFlee,
}

func ActionKeys() []string { return append([]string(nil), actionKeys...) }
