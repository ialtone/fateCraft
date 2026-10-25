// Package bootstrap is the application composition root for trusted gameplay.
// The server, importer and integration tests use the same installed catalogue.
package bootstrap

import (
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/campaigngeneric"
	"github.com/ialtone/fatecraft/internal/gameplay/campaignruntime"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldinit"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldview"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaignactions"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigninit"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaignview"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trial"
	"github.com/ialtone/fatecraft/internal/gameplay/dungeon"
	"github.com/ialtone/fatecraft/internal/gameplay/management"
	"github.com/ialtone/fatecraft/internal/gameplay/relationship"
	"github.com/ialtone/fatecraft/internal/gameplay/sect"
	"github.com/ialtone/fatecraft/internal/gameplay/stealth"
	"github.com/ialtone/fatecraft/internal/gameplay/survival"
	"github.com/ialtone/fatecraft/internal/gameplay/tomb"
)

func NewRegistry() *contract.Registry {
	registry := contract.NewCoreRegistry()
	campaigngeneric.Register(registry)
	cultivation.Register(registry)
	trial.Register(registry)
	registry.RegisterModule(survival.Descriptor())
	registry.RegisterModule(management.Descriptor())
	registry.RegisterModule(stealth.Descriptor())
	registry.RegisterModule(relationship.Descriptor())
	registry.RegisterModule(tomb.Descriptor())
	registry.RegisterModule(dungeon.Descriptor())
	registry.RegisterModule(sect.Descriptor())
	// Import and server startup must agree on the executable catalogue.
	probe := campaign.NewService(nil, nil, nil, nil, registry)
	InstallCampaign(probe)
	if err := probe.ValidateInstallation(registry); err != nil {
		panic(err)
	}
	return registry
}

func InstallCampaign(service *campaign.Service) {
	worldactions.Register(service, contract.EngineRef{Key: campaigngeneric.EngineKey, Version: 1})
	campaignactions.Register(service)
	service.RegisterInitializer(campaigngeneric.LifecycleHandler, worldinit.Initialize)
	service.RegisterInitializer(cultivation.CampaignLifecycleHandler, campaigninit.Initialize)
	service.RegisterProjection(campaigngeneric.ProjectionHandler, worldview.Provider{})
	service.RegisterProjection(cultivation.CampaignProjectionHandler, campaignview.Provider{})
	if registry := service.Registry(); registry != nil {
		installRuntimeCampaigns(service, registry)
	}
}

func installRuntimeCampaigns(service *campaign.Service, registry *contract.Registry) {
	for _, descriptor := range registry.Modules() {
		campaignruntime.Install(service, descriptor, registry)
	}
}
