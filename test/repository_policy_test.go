package test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseNotesUseImmutableImageTags(t *testing.T) {
	workflow, err := os.ReadFile("../.github/workflows/build.yaml")
	require.NoError(t, err)

	_, releaseStep, found := strings.Cut(string(workflow), "cat > release-notes.md")
	require.True(t, found)
	releaseNotes, _, found := strings.Cut(releaseStep, "echo \"version=")
	require.True(t, found)

	require.NotContains(t, releaseNotes, ":latest")
	require.Equal(t, 2, strings.Count(releaseNotes, ":${CURRENT_TAG}"))
}

func TestDockerContextExcludesLocalArtifacts(t *testing.T) {
	contents, err := os.ReadFile("../.dockerignore")
	require.NoError(t, err)

	patterns := strings.Fields(string(contents))
	for _, pattern := range []string{
		".git/",
		"bin/",
		"coverage/",
		"echo-server",
		"flow-generator",
	} {
		require.Contains(t, patterns, pattern)
	}
}
