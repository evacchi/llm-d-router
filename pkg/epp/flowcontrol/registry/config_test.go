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

package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/flowcontrol/contracts"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/flowcontrol/framework/plugins/queue"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/framework/interface/flowcontrol"
	fwkfcmocks "github.com/llm-d/llm-d-inference-scheduler/pkg/epp/framework/interface/flowcontrol/mocks"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/framework/interface/plugin"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/framework/plugins/flowcontrol/fairness/globalstrict"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/framework/plugins/flowcontrol/fairness/roundrobin"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/framework/plugins/flowcontrol/ordering/edf"
	"github.com/llm-d/llm-d-inference-scheduler/pkg/epp/framework/plugins/flowcontrol/ordering/fcfs"
)

var (
	mockFCFSOrderingPolicy = &fwkfcmocks.MockOrderingPolicy{
		TypedNameV: plugin.TypedName{
			Type: fcfs.FCFSOrderingPolicyType,
			Name: fcfs.FCFSOrderingPolicyType,
		},
	}
	mockEDFOrderingPolicy = &fwkfcmocks.MockOrderingPolicy{
		TypedNameV: plugin.TypedName{
			Type: edf.EDFOrderingPolicyType,
			Name: edf.EDFOrderingPolicyType,
		},
		RequiredQueueCapabilitiesV: []flowcontrol.QueueCapability{flowcontrol.CapabilityPriorityConfigurable},
	}
	mockGlobalStrictFairnessPolicy = &fwkfcmocks.MockFairnessPolicy{
		TypedNameV: plugin.TypedName{
			Type: globalstrict.GlobalStrictFairnessPolicyType,
			Name: globalstrict.GlobalStrictFairnessPolicyType,
		},
	}
	mockRoundRobinFairnessPolicy = &fwkfcmocks.MockFairnessPolicy{
		TypedNameV: plugin.TypedName{
			Type: roundrobin.RoundRobinFairnessPolicyType,
			Name: roundrobin.RoundRobinFairnessPolicyType,
		},
	}
)

func newTestPolicyDefaults() *PolicyDefaults {
	return &PolicyDefaults{
		OrderingPolicy: mockFCFSOrderingPolicy,
		FairnessPolicy: mockGlobalStrictFairnessPolicy,
	}
}

// mockCapabilityChecker is a test double for verifying that NewConfig correctly delegates compatibility checks.
type mockCapabilityChecker struct {
	checkCompatibilityFunc func(p flowcontrol.OrderingPolicy, q queue.RegisteredQueueName) error
}

func (m *mockCapabilityChecker) CheckCompatibility(p flowcontrol.OrderingPolicy, q queue.RegisteredQueueName) error {
	if m.checkCompatibilityFunc != nil {
		return m.checkCompatibilityFunc(p, q)
	}
	return nil
}

// mustBand is a helper to simplify test table setup.
// It panics if the band config creation fails, which should not happen with valid static inputs.
func mustBand(t *testing.T, priority int, opts ...PriorityBandConfigOption) *PriorityBandConfig {
	defaults := newTestPolicyDefaults()
	pb, err := NewPriorityBandConfig(defaults, priority, opts...)
	require.NoError(t, err, "failed to create test band")
	return pb
}

func TestNewConfig(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		opts          []ConfigOption
		defaults      *PolicyDefaults
		expectErr     bool
		expectedErrIs error // Optional: check for specific wrapped error
		assertion     func(*testing.T, *Config)
	}{
		// --- Success Paths ---
		{
			name: "ShouldApplySystemDefaults_WhenNoOptionsProvided",
			opts: []ConfigOption{
				WithPriorityBand(mustBand(t, 1)),
			},
			defaults: newTestPolicyDefaults(),
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, defaultInitialShardCount, cfg.InitialShardCount, "InitialShardCount should be defaulted")
				assert.Equal(t, defaultFlowGCTimeout, cfg.FlowGCTimeout, "FlowGCTimeout should be defaulted")
				assert.Equal(t, defaultPriorityBandGCTimeout, cfg.PriorityBandGCTimeout, "PriorityBandGCTimeout should be defaulted")

				// Verify Band Defaults
				require.Contains(t, cfg.PriorityBands, 1)
				band := cfg.PriorityBands[1]
				assert.Equal(t, DefaultOrderingPolicyRef, band.OrderingPolicy.TypedName().Name)
				require.NotNil(t, band.FairnessPolicy)
				assert.Equal(t, DefaultFairnessPolicyRef, band.FairnessPolicy.TypedName().Name)
				assert.Equal(t, defaultQueue, band.Queue)
				assert.Equal(t, defaultPriorityBandMaxBytes, band.MaxBytes)
			},
		},
		{
			name: "ShouldRespectGlobalOverrides",
			opts: []ConfigOption{
				WithInitialShardCount(10),
				WithMaxBytes(5000),
				WithFlowGCTimeout(1 * time.Hour),
				WithPriorityBandGCTimeout(2 * time.Hour),
				WithPriorityBand(mustBand(t, 1)),
			},
			defaults: newTestPolicyDefaults(),
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 10, cfg.InitialShardCount)
				assert.Equal(t, uint64(5000), cfg.MaxBytes)
				assert.Equal(t, 1*time.Hour, cfg.FlowGCTimeout)
				assert.Equal(t, 2*time.Hour, cfg.PriorityBandGCTimeout)
			},
		},
		{
			name: "ShouldApplyBandDefaults_WithRawStructLiterals",
			opts: []ConfigOption{
				WithPriorityBand(&PriorityBandConfig{Priority: 1}),
			},
			defaults: newTestPolicyDefaults(),
			assertion: func(t *testing.T, cfg *Config) {
				require.Contains(t, cfg.PriorityBands, 1)
				band := cfg.PriorityBands[1]
				assert.Equal(t, defaultQueue, band.Queue, "Queue should be defaulted even for raw struct inputs")
				assert.NotNil(t, band.FairnessPolicy)
				assert.Equal(t, DefaultFairnessPolicyRef, band.FairnessPolicy.TypedName().Name)
				assert.Equal(t, DefaultOrderingPolicyRef, band.OrderingPolicy.TypedName().Name)
			},
		},
		{
			name: "ShouldSucceed_WhenNoPriorityBandsDefined_WithDynamicDefaults",
			opts: []ConfigOption{
				// No WithPriorityBand options provided.
				// This relies entirely on dynamic provisioning.
			},
			defaults: newTestPolicyDefaults(),
			assertion: func(t *testing.T, cfg *Config) {
				assert.Empty(t, cfg.PriorityBands, "PriorityBands map should be empty")
				require.NotNil(t, cfg.DefaultPriorityBand, "DefaultPriorityBand template must be initialized")
				assert.Equal(t, defaultQueue, cfg.DefaultPriorityBand.Queue)
				assert.NotNil(t, cfg.DefaultPriorityBand.FairnessPolicy)
				assert.Equal(t, DefaultFairnessPolicyRef, cfg.DefaultPriorityBand.FairnessPolicy.TypedName().Name)
			},
		},
		{
			name: "ShouldRespectCustomDefaultPriorityBand",
			opts: []ConfigOption{
				WithDefaultPriorityBand(&PriorityBandConfig{
					Queue: "CustomQueue",
				}),
				withCapabilityChecker(&mockCapabilityChecker{
					checkCompatibilityFunc: func(flowcontrol.OrderingPolicy, queue.RegisteredQueueName) error { return nil },
				}),
			},
			defaults: newTestPolicyDefaults(),
			assertion: func(t *testing.T, cfg *Config) {
				require.NotNil(t, cfg.DefaultPriorityBand)
				assert.Equal(t, queue.RegisteredQueueName("CustomQueue"), cfg.DefaultPriorityBand.Queue)
				assert.NotNil(t, cfg.DefaultPriorityBand.FairnessPolicy)
				assert.Equal(t, DefaultFairnessPolicyRef, cfg.DefaultPriorityBand.FairnessPolicy.TypedName().Name)
				assert.Equal(t, DefaultOrderingPolicyRef, cfg.DefaultPriorityBand.OrderingPolicy.TypedName().Name)
			},
		},

		// --- Validation Errors (Global) ---
		{
			name:      "ShouldError_WhenInitialShardCountIsInvalid",
			opts:      []ConfigOption{WithInitialShardCount(0)}, // Option itself should return error.
			defaults:  newTestPolicyDefaults(),
			expectErr: true,
		},
		{
			name:      "ShouldError_WhenFlowGCTimeoutIsInvalid",
			opts:      []ConfigOption{WithFlowGCTimeout(-1 * time.Second)},
			defaults:  newTestPolicyDefaults(),
			expectErr: true,
		},
		{
			name:      "ShouldError_WhenPriorityBandGCTimeoutIsNegative",
			opts:      []ConfigOption{WithPriorityBandGCTimeout(-1 * time.Second)},
			defaults:  newTestPolicyDefaults(),
			expectErr: true,
		},
		{
			name: "ShouldError_WhenPriorityBandGCTimeoutLessThanFlowGCTimeout",
			opts: []ConfigOption{
				WithFlowGCTimeout(10 * time.Minute),
				WithPriorityBandGCTimeout(5 * time.Minute), // Less than flow timeout
			},
			defaults:  newTestPolicyDefaults(),
			expectErr: true,
		},
		{
			name: "ShouldSucceed_WhenPriorityBandGCTimeoutEqualToFlowGCTimeout",
			opts: []ConfigOption{
				WithFlowGCTimeout(10 * time.Minute),
				WithPriorityBandGCTimeout(10 * time.Minute), // Equal is OK
			},
			defaults: newTestPolicyDefaults(),
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 10*time.Minute, cfg.PriorityBandGCTimeout)
			},
		},
		{
			name: "ShouldSucceed_WhenPriorityBandGCTimeoutGreaterThanFlowGCTimeout",
			opts: []ConfigOption{
				WithFlowGCTimeout(5 * time.Minute),
				WithPriorityBandGCTimeout(15 * time.Minute),
			},
			defaults: newTestPolicyDefaults(),
			assertion: func(t *testing.T, cfg *Config) {
				assert.Equal(t, 15*time.Minute, cfg.PriorityBandGCTimeout)
			},
		},

		// --- Validation Errors (Bands) ---
		{
			name: "ShouldError_WhenDuplicatePriorityLevelAdded",
			opts: []ConfigOption{
				WithPriorityBand(mustBand(t, 1)),
				WithPriorityBand(mustBand(t, 1)), // Same priority level
			},
			defaults:  newTestPolicyDefaults(),
			expectErr: true,
		},
		{
			name:      "ShouldError_WhenBandIsNil",
			opts:      []ConfigOption{WithPriorityBand(nil)},
			defaults:  newTestPolicyDefaults(),
			expectErr: true,
		},

		// --- Hydration Failures ---
		{
			name:      "ShouldError_WhenDefaultPolicyMissingFromDefaults",
			opts:      []ConfigOption{WithPriorityBand(&PriorityBandConfig{Priority: 1})},
			defaults:  &PolicyDefaults{}, // Empty defaults: no policies.
			expectErr: true,
		},

		// --- Compatibility Checks ---
		{
			name: "ShouldError_WhenCapabilityCheckerFails",
			opts: []ConfigOption{
				WithPriorityBand(mustBand(t, 1)),
				withCapabilityChecker(&mockCapabilityChecker{
					checkCompatibilityFunc: func(flowcontrol.OrderingPolicy, queue.RegisteredQueueName) error {
						return contracts.ErrPolicyQueueIncompatible
					},
				}),
			},
			defaults:      newTestPolicyDefaults(),
			expectErr:     true,
			expectedErrIs: contracts.ErrPolicyQueueIncompatible,
		},
		{
			name: "ShouldError_WhenDefaultRuntimeCheckerDetectsUnknownQueue",
			opts: []ConfigOption{
				WithPriorityBand(mustBand(t, 1, WithQueue("non-existent-queue"))),
			},
			defaults:  newTestPolicyDefaults(),
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := NewConfig(tc.defaults, tc.opts...)

			if tc.expectErr {
				require.Error(t, err, "expected validation error")
				if tc.expectedErrIs != nil {
					assert.ErrorIs(t, err, tc.expectedErrIs)
				}
				assert.Nil(t, cfg, "config should be nil on error")
			} else {
				require.NoError(t, err, "unexpected configuration error")
				require.NotNil(t, cfg, "config should not be nil on success")
				if tc.assertion != nil {
					tc.assertion(t, cfg)
				}
			}
		})
	}
}

func TestNewPriorityBandConfig(t *testing.T) {
	t.Parallel()
	defaults := newTestPolicyDefaults()

	t.Run("ShouldApplyUserOverrides", func(t *testing.T) {
		t.Parallel()
		pb, err := NewPriorityBandConfig(defaults, 1,
			WithQueue(queue.RegisteredQueueName("CustomQueue")),
			WithBandMaxBytes(999),
			WithOrderingPolicy(mockEDFOrderingPolicy),
			WithFairnessPolicy(mockRoundRobinFairnessPolicy),
		)
		require.NoError(t, err)
		assert.Equal(t, queue.RegisteredQueueName("CustomQueue"), pb.Queue)
		assert.Equal(t, uint64(999), pb.MaxBytes)
		require.NotNil(t, pb.OrderingPolicy)
		assert.Equal(t, edf.EDFOrderingPolicyType, pb.OrderingPolicy.TypedName().Name)
		require.NotNil(t, pb.FairnessPolicy)
		assert.Equal(t, roundrobin.RoundRobinFairnessPolicyType, pb.FairnessPolicy.TypedName().Name)
	})

	t.Run("ShouldError_OnInvalidOptions", func(t *testing.T) {
		t.Parallel()
		pb, err := NewPriorityBandConfig(defaults, 1, WithQueue(""))
		assert.Error(t, err, "Should error when setting empty queue")
		assert.Nil(t, pb)
	})

	t.Run("ShouldError_WhenNilOrderingPolicy", func(t *testing.T) {
		t.Parallel()
		pb, err := NewPriorityBandConfig(defaults, 1,
			WithOrderingPolicy(nil),
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ordering policy cannot be nil")
		assert.Nil(t, pb)
	})

	t.Run("ShouldError_WhenNilFairnessPolicy", func(t *testing.T) {
		t.Parallel()
		pb, err := NewPriorityBandConfig(defaults, 1,
			WithFairnessPolicy(nil),
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "fairness policy cannot be nil")
		assert.Nil(t, pb)
	})

	t.Run("ShouldDefaultToHeap_WhenPolicyRequiresIt", func(t *testing.T) {
		t.Parallel()
		pb, err := NewPriorityBandConfig(defaults, 10,
			WithOrderingPolicy(mockEDFOrderingPolicy),
			WithFairnessPolicy(mockGlobalStrictFairnessPolicy),
		)
		require.NoError(t, err)
		assert.Equal(t, queue.RegisteredQueueName(queue.MaxMinHeapName), pb.Queue,
			"EDF requires PriorityConfigurable, so should default to MaxMinHeap")
	})

	t.Run("ShouldDefaultToList_WhenPolicyDoesNotRequirePriority", func(t *testing.T) {
		t.Parallel()
		pb, err := NewPriorityBandConfig(defaults, 20,
			WithOrderingPolicy(mockFCFSOrderingPolicy),
			WithFairnessPolicy(mockGlobalStrictFairnessPolicy),
		)
		require.NoError(t, err)
		assert.Equal(t, queue.RegisteredQueueName(queue.ListQueueName), pb.Queue,
			"FCFS does not require PriorityConfigurable, so should default to ListQueue")
	})
}

func TestConfig_Partition(t *testing.T) {
	t.Parallel()
	defaults := newTestPolicyDefaults()

	// Setup:
	// Global: 103 MaxBytes.
	// Band 1: 55 MaxBytes.
	// Band 2: 0 MaxBytes (will default to 1GB).
	// Band 3: 20 MaxBytes.
	cfg, err := NewConfig(
		defaults,
		WithMaxBytes(103),
		WithPriorityBand(mustBand(t, 1, WithBandMaxBytes(55))),
		WithPriorityBand(mustBand(t, 2, WithBandMaxBytes(0))), // Explicit 0 implies default behavior via logic.
		WithPriorityBand(mustBand(t, 3, WithBandMaxBytes(20))),
	)
	require.NoError(t, err)

	// NewConfig applies defaults. If we passed 0 to NewPriorityBandConfig, it became 1GB.
	// We need to check what the setup resulted in.
	expectedBand2Total := defaultPriorityBandMaxBytes
	assert.Equal(t, expectedBand2Total, cfg.PriorityBands[2].MaxBytes, "Band 2 should have been defaulted")

	t.Run("ShouldDistributeRemainderCorrectly", func(t *testing.T) {
		t.Parallel()
		totalShards := 10

		// Global: 103 / 10 = 10 rem 3. First 3 shards get 11.
		// Band 1: 55 / 10 = 5 rem 5. First 5 shards get 6.
		// Band 3: 20 / 10 = 2 rem 0. All get 2.

		var sumGlobal, sumBand1, sumBand2, sumBand3 uint64

		for i := range totalShards {
			shard := cfg.partition(i, totalShards)
			require.NotNil(t, shard)

			// Accumulate.
			sumGlobal += shard.MaxBytes
			sumBand1 += shard.PriorityBands[1].MaxBytes
			sumBand2 += shard.PriorityBands[2].MaxBytes
			sumBand3 += shard.PriorityBands[3].MaxBytes

			// Spot check specific shards.
			if i < 3 {
				assert.Equal(t, uint64(11), shard.MaxBytes, "Shard %d global bytes mismatch", i)
			} else {
				assert.Equal(t, uint64(10), shard.MaxBytes, "Shard %d global bytes mismatch", i)
			}
		}

		// Verify totals.
		assert.Equal(t, cfg.MaxBytes, sumGlobal, "Total global bytes preserved")
		assert.Equal(t, cfg.PriorityBands[1].MaxBytes, sumBand1, "Total Band 1 bytes preserved")
		assert.Equal(t, cfg.PriorityBands[2].MaxBytes, sumBand2, "Total Band 2 bytes preserved")
		assert.Equal(t, cfg.PriorityBands[3].MaxBytes, sumBand3, "Total Band 3 bytes preserved")
	})
}

func TestConfig_Clone(t *testing.T) {
	t.Parallel()
	defaults := newTestPolicyDefaults()

	original, err := NewConfig(
		defaults,
		WithMaxBytes(1000),
		WithPriorityBand(mustBand(t, 1)),
		WithPriorityBand(mustBand(t, 2)),
	)
	require.NoError(t, err, "Setup failed")

	t.Run("ShouldReturnNil_ForNilReceiver", func(t *testing.T) {
		var nilConfig *Config
		assert.Nil(t, nilConfig.Clone())
	})

	t.Run("ShouldCreateDeepCopy", func(t *testing.T) {
		clone := original.Clone()

		require.NotSame(t, original, clone, "Struct pointers should differ")
		require.NotSame(t, original.PriorityBands[1], clone.PriorityBands[1],
			"Map values (pointers to bands) should differ")
		assert.Equal(t, original.MaxBytes, clone.MaxBytes)
	})

	t.Run("ShouldIsolateModifications", func(t *testing.T) {
		clone := original.Clone()

		// Modify the clone's map entry.
		clone.PriorityBands[1].MaxBytes = 99999

		assert.Equal(t, defaultPriorityBandMaxBytes, original.PriorityBands[1].MaxBytes)
		assert.Equal(t, uint64(99999), clone.PriorityBands[1].MaxBytes)
	})

	t.Run("ShouldDeepCopyDefaultPriorityBand", func(t *testing.T) {
		t.Parallel()
		original, err := NewConfig(newTestPolicyDefaults())
		require.NoError(t, err)

		clone := original.Clone()

		require.NotSame(t, original.DefaultPriorityBand, clone.DefaultPriorityBand,
			"Clone should have a distinct pointer for DefaultPriorityBand")
	})
}
