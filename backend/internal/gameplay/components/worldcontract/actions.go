package worldcontract

const (
	ContextPolicy           = "campaign.generic_context"
	ActionGainItem          = "inventory.gain_item"
	ActionTradeBuy          = "trade.buy"
	ActionTradeSell         = "trade.sell"
	ActionAdjustMarket      = "market.adjust_price_modifier"
	ActionTravel            = "travel.move"
	ActionInvestigate       = "world.investigate"
	ActionResolveUncertain  = "fortune.resolve_uncertain"
	ActionInstantiateNPC    = "npc.instantiate_archetype"
	ActionMoveNPC           = "npc.move"
	ActionSetNPCStatus      = "npc.set_status"
	ActionRecordInteraction = "relationship.record_interaction"
	ActionAdjustFaction     = "faction.adjust_standing"
	ActionAcceptQuest       = "quest.accept"
	ActionSubmitQuest       = "quest.submit"
)
