package contract

const CampaignModuleRandomEvent = "core.module_random"

// WithCampaignRuntime declares the standard Campaign adapter for a pure
// RuntimeModule. The composition root installs the actual adapter functions.
func WithCampaignRuntime(module ModuleDescriptor) ModuleDescriptor {
	module.CampaignLifecycleHandlers = []string{module.Key + ".lifecycle"}
	module.CampaignProjectionHandlers = []string{module.Key + ".projection"}
	module.CampaignContextPolicies = []string{module.Key + ".context"}
	module.CampaignActions = nil
	module.CampaignActionWrites = map[string][]string{}
	module.CampaignActionEvents = map[string][]string{}
	for _, action := range module.Runtime.Actions() {
		module.CampaignActions = append(module.CampaignActions, action.Key)
		module.CampaignActionWrites[action.Key] = []string{"gameplay"}
		module.CampaignActionEvents[action.Key] = append(append([]string{}, action.Events...), CampaignModuleRandomEvent)
	}
	return module
}
