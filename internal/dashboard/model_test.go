package dashboard

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	statusapi "github.com/PhilipSchmid/flow-generator-app/internal/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitorShortcuts(t *testing.T) {
	model := Model{}

	model = updateWithKey(t, model, tea.Key{Code: tea.KeyTab})
	assert.Equal(t, 1, model.windowIndex)
	model.windowIndex = len(windows) - 1
	model = updateWithKey(t, model, tea.Key{Code: tea.KeyTab})
	assert.Zero(t, model.windowIndex)
	model = updateWithKey(t, model, tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	assert.Equal(t, len(windows)-1, model.windowIndex)

	model = updateWithKey(t, model, tea.Key{Code: tea.KeyF1})
	assert.True(t, model.showHelp)
	model = updateWithKey(t, model, tea.Key{Code: tea.KeyRight})
	assert.Equal(t, len(windows)-1, model.windowIndex, "help modal should capture navigation keys")
	model = updateWithKey(t, model, tea.Key{Code: tea.KeyEscape})
	assert.False(t, model.showHelp)
	model = updateWithKey(t, model, tea.Key{Code: tea.KeyF1})

	_, quit := model.Update(tea.KeyPressMsg(tea.Key{Text: "q", Code: 'q'}))
	assert.NotNil(t, quit)
}

func TestManualRefreshDoesNotStartAnotherRefreshLoop(t *testing.T) {
	now := time.Now().UTC()
	snapshot := statusapi.Snapshot{Role: "client", StartedAt: now, SampledAt: now, Client: &statusapi.ClientSnapshot{}}

	_, manualCommand := (Model{}).Update(fetchResult{snapshot: snapshot})
	assert.Nil(t, manualCommand)
	_, scheduledCommand := (Model{}).Update(fetchResult{snapshot: snapshot, continueRefresh: true})
	assert.NotNil(t, scheduledCommand)
}

func TestRefreshesAreCoalescedWhileFetchIsRunning(t *testing.T) {
	model := Model{fetching: true}
	model = updateWithKey(t, model, tea.Key{Text: "r", Code: 'r'})
	assert.True(t, model.fetching)
	assert.False(t, model.refreshDue)

	updated, command := model.Update(refreshMsg(time.Now()))
	model = updated.(Model)
	assert.Nil(t, command)
	assert.True(t, model.refreshDue)

	now := time.Now().UTC()
	updated, command = model.Update(fetchResult{snapshot: statusapi.Snapshot{
		Role: "client", StartedAt: now, SampledAt: now, Client: &statusapi.ClientSnapshot{},
	}})
	model = updated.(Model)
	assert.NotNil(t, command)
	assert.True(t, model.fetching)
	assert.False(t, model.refreshDue)
}

func TestStaleFetchResultDoesNotReplaceCurrentSnapshot(t *testing.T) {
	now := time.Now().UTC()
	current := statusapi.Snapshot{Role: "client", StartedAt: now, SampledAt: now, Client: &statusapi.ClientSnapshot{FlowsStarted: 2}}
	model := Model{snapshot: &current, connected: true}

	updated, _ := model.Update(fetchResult{snapshot: statusapi.Snapshot{
		Role: "client", StartedAt: now, SampledAt: now.Add(-time.Second), Client: &statusapi.ClientSnapshot{FlowsStarted: 1},
	}})
	model = updated.(Model)
	require.NotNil(t, model.snapshot)
	assert.Equal(t, uint64(2), model.snapshot.Client.FlowsStarted)
}

func updateWithKey(t *testing.T, model Model, key tea.Key) Model {
	t.Helper()
	updated, _ := model.Update(tea.KeyPressMsg(key))
	result, ok := updated.(Model)
	require.True(t, ok)
	return result
}
