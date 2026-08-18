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

	model = updateWithKey(t, model, tea.Key{Code: tea.KeySpace})
	assert.True(t, model.paused)
	model = updateWithKey(t, model, tea.Key{Code: tea.KeyF1})
	assert.True(t, model.showHelp)

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

func updateWithKey(t *testing.T, model Model, key tea.Key) Model {
	t.Helper()
	updated, _ := model.Update(tea.KeyPressMsg(key))
	result, ok := updated.(Model)
	require.True(t, ok)
	return result
}
