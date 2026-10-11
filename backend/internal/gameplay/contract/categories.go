package contract

// Categories organize the product catalogue; they never select rule handlers.
// A category may contain several independently versioned modules and modes.
type Category struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func Categories() []Category {
	return []Category{
		{Key: "survival", Name: "生存探索"},
		{Key: "management", Name: "经营建设"},
		{Key: "stealth", Name: "潜入逃脱"},
		{Key: "relationship", Name: "关系叙事"},
		{Key: "tomb", Name: "古墓探险"},
		{Key: "dungeon", Name: "地牢远征"},
		{Key: "cultivation", Name: "修仙飞升"},
		{Key: "sect", Name: "宗门经营"},
	}
}

func validCategory(key string) bool {
	if key == "" {
		return true
	} // Infrastructure modules are not product categories.
	for _, category := range Categories() {
		if category.Key == key {
			return true
		}
	}
	return false
}
