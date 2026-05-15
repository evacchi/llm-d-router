/*
Copyright 2025 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package loader

import (
	"fmt"

	configapi "github.com/llm-d/llm-d-inference-scheduler/apix/config/v1alpha1"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/flowcontrol"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/flowcontrol/controller"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/flowcontrol/registry"
	fwkfc "github.com/llm-d/llm-d-inference-scheduler/pkg/epp/framework/interface/flowcontrol"
	fwkplugin "github.com/llm-d/llm-d-inference-scheduler/pkg/epp/framework/interface/plugin"
)

func buildFlowControlConfig(
	apiConfig *configapi.FlowControlConfig,
	handle fwkplugin.Handle,
) (*flowcontrol.Config, error) {
	defaults, err := resolveFlowControlDefaults(handle)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve flow control defaults: %w", err)
	}

	registryConfig, err := buildRegistryConfig(apiConfig, handle, defaults)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry config: %w", err)
	}

	ctrlCfg, err := controller.NewConfigFromAPI(apiConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create controller config: %w", err)
	}

	usageLimitPolicy, err := resolveUsageLimitPolicy(apiConfig, handle)
	if err != nil {
		return nil, err
	}

	return &flowcontrol.Config{
		Controller:       ctrlCfg,
		Registry:         registryConfig,
		UsageLimitPolicy: usageLimitPolicy,
	}, nil
}

func buildRegistryConfig(
	apiConfig *configapi.FlowControlConfig,
	handle fwkplugin.Handle,
	defaults *registry.PolicyDefaults,
) (*registry.Config, error) {
	if apiConfig == nil {
		return registry.NewConfig(defaults)
	}

	opts := make([]registry.ConfigOption, 0, len(apiConfig.PriorityBands)+3)

	maxBytes, err := registry.ResolveQuantity(apiConfig.MaxBytes, "global MaxBytes")
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 {
		opts = append(opts, registry.WithMaxBytes(maxBytes))
	}

	maxRequests, err := registry.ResolveQuantity(apiConfig.MaxRequests, "global MaxRequests")
	if err != nil {
		return nil, err
	}
	if maxRequests > 0 {
		opts = append(opts, registry.WithMaxRequests(maxRequests))
	}

	if apiConfig.DefaultPriorityBand != nil {
		templateBand, err := buildDefaultPriorityBandTemplate(handle, defaults, apiConfig.DefaultPriorityBand)
		if err != nil {
			return nil, err
		}
		opts = append(opts, registry.WithDefaultPriorityBand(templateBand))
	}

	for _, band := range apiConfig.PriorityBands {
		pb, err := buildPriorityBand(handle, defaults, band)
		if err != nil {
			return nil, err
		}
		opts = append(opts, registry.WithPriorityBand(pb))
	}

	return registry.NewConfig(defaults, opts...)
}

func buildDefaultPriorityBandTemplate(
	handle fwkplugin.Handle,
	defaults *registry.PolicyDefaults,
	apiBand *configapi.PriorityBandConfig,
) (*registry.PriorityBandConfig, error) {
	bandOpts := make([]registry.PriorityBandConfigOption, 0, 4)

	maxBytes, err := registry.ResolveQuantity(apiBand.MaxBytes, "DefaultPriorityBand MaxBytes")
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 {
		bandOpts = append(bandOpts, registry.WithBandMaxBytes(maxBytes))
	}

	maxRequests, err := registry.ResolveQuantity(apiBand.MaxRequests, "DefaultPriorityBand MaxRequests")
	if err != nil {
		return nil, err
	}
	if maxRequests > 0 {
		bandOpts = append(bandOpts, registry.WithBandMaxRequests(maxRequests))
	}

	if apiBand.OrderingPolicyRef != "" {
		policy, err := resolveOrderingPolicy(apiBand.OrderingPolicyRef, handle)
		if err != nil {
			return nil, err
		}
		bandOpts = append(bandOpts, registry.WithOrderingPolicy(policy))
	}
	if apiBand.FairnessPolicyRef != "" {
		policy, err := resolveFairnessPolicy(apiBand.FairnessPolicyRef, handle)
		if err != nil {
			return nil, err
		}
		bandOpts = append(bandOpts, registry.WithFairnessPolicy(policy))
	}

	templateBand, err := registry.NewPriorityBandConfig(defaults, 0, bandOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create default priority band template: %w", err)
	}
	return templateBand, nil
}

func buildPriorityBand(
	handle fwkplugin.Handle,
	defaults *registry.PolicyDefaults,
	band configapi.PriorityBandConfig,
) (*registry.PriorityBandConfig, error) {
	bandOpts := make([]registry.PriorityBandConfigOption, 0, 4)

	maxBytes, err := registry.ResolveQuantity(band.MaxBytes, fmt.Sprintf("priority band %d MaxBytes", band.Priority))
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 {
		bandOpts = append(bandOpts, registry.WithBandMaxBytes(maxBytes))
	}

	maxRequests, err := registry.ResolveQuantity(band.MaxRequests, fmt.Sprintf("priority band %d MaxRequests", band.Priority))
	if err != nil {
		return nil, err
	}
	if maxRequests > 0 {
		bandOpts = append(bandOpts, registry.WithBandMaxRequests(maxRequests))
	}

	if band.OrderingPolicyRef != "" {
		policy, err := resolveOrderingPolicy(band.OrderingPolicyRef, handle)
		if err != nil {
			return nil, err
		}
		bandOpts = append(bandOpts, registry.WithOrderingPolicy(policy))
	}
	if band.FairnessPolicyRef != "" {
		policy, err := resolveFairnessPolicy(band.FairnessPolicyRef, handle)
		if err != nil {
			return nil, err
		}
		bandOpts = append(bandOpts, registry.WithFairnessPolicy(policy))
	}

	pb, err := registry.NewPriorityBandConfig(defaults, band.Priority, bandOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create priority band config for priority %d: %w", band.Priority, err)
	}
	return pb, nil
}

func resolveFlowControlDefaults(handle fwkplugin.Handle) (*registry.PolicyDefaults, error) {
	op, err := resolveOrderingPolicy(registry.DefaultOrderingPolicyRef, handle)
	if err != nil {
		return nil, err
	}
	fp, err := resolveFairnessPolicy(registry.DefaultFairnessPolicyRef, handle)
	if err != nil {
		return nil, err
	}
	return &registry.PolicyDefaults{
		OrderingPolicy: op,
		FairnessPolicy: fp,
	}, nil
}

func resolveOrderingPolicy(ref string, handle fwkplugin.Handle) (fwkfc.OrderingPolicy, error) {
	p := handle.Plugin(ref)
	if p == nil {
		return nil, fmt.Errorf("no ordering policy registered for name %q", ref)
	}
	policy, ok := p.(fwkfc.OrderingPolicy)
	if !ok {
		return nil, fmt.Errorf("plugin %q is not a flowcontrol.OrderingPolicy (type: %T)", ref, p)
	}
	return policy, nil
}

func resolveFairnessPolicy(ref string, handle fwkplugin.Handle) (fwkfc.FairnessPolicy, error) {
	p := handle.Plugin(ref)
	if p == nil {
		return nil, fmt.Errorf("no fairness policy registered for name %q", ref)
	}
	policy, ok := p.(fwkfc.FairnessPolicy)
	if !ok {
		return nil, fmt.Errorf("plugin %q is not a flowcontrol.FairnessPolicy (type: %T)", ref, p)
	}
	return policy, nil
}

func resolveUsageLimitPolicy(apiConfig *configapi.FlowControlConfig, handle fwkplugin.Handle) (fwkfc.UsageLimitPolicy, error) {
	ref := registry.DefaultUsageLimitPolicyRef
	if apiConfig != nil && apiConfig.UsageLimitPolicyPluginRef != "" {
		ref = apiConfig.UsageLimitPolicyPluginRef
	}
	p := handle.Plugin(ref)
	if p == nil {
		return nil, fmt.Errorf("usage limit policy plugin '%s' not found", ref)
	}
	usageLimitPolicy, ok := p.(fwkfc.UsageLimitPolicy)
	if !ok {
		return nil, fmt.Errorf("plugin '%s' does not implement UsageLimitPolicy", ref)
	}
	return usageLimitPolicy, nil
}
