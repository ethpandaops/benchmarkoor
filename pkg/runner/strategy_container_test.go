package runner

import (
	"testing"

	"github.com/ethpandaops/benchmarkoor/pkg/config"
	"github.com/ethpandaops/benchmarkoor/pkg/executor"
	"github.com/stretchr/testify/require"
)

func TestSuiteSegmentContainerRecreation(t *testing.T) {
	tests := []*executor.TestWithSteps{
		{Name: "segment-zero", Metadata: map[string]string{suiteSegmentStartMetadata: "true"}},
		{Name: "same-segment"},
		{Name: "segment-one", Metadata: map[string]string{suiteSegmentStartMetadata: "TRUE"}},
	}

	require.True(t, usesSuiteSegmentBoundaries(tests))
	require.False(t, shouldRecreateContainer(
		config.RollbackStrategyContainerRecreate, 0, tests[0], true,
	))
	require.False(t, shouldRecreateContainer(
		config.RollbackStrategyContainerRecreate, 1, tests[1], true,
	))
	require.True(t, shouldRecreateContainer(
		config.RollbackStrategyContainerRecreate, 2, tests[2], true,
	))
}

func TestContainerRecreationWithoutSegmentsRemainsPerTest(t *testing.T) {
	test := &executor.TestWithSteps{Name: "second"}

	require.False(t, usesSuiteSegmentBoundaries([]*executor.TestWithSteps{test}))
	require.True(t, shouldRecreateContainer(
		config.RollbackStrategyContainerRecreate, 1, test, false,
	))
	require.False(t, shouldRecreateContainer(config.RollbackStrategyNone, 1, test, false))
}
