package version

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInfo(t *testing.T) {
	// Save original values
	origVersion := Version
	origGitCommit := GitCommit
	origBuildDate := BuildDate

	// Set test values
	Version = "v1.2.3"
	GitCommit = "abc123"
	BuildDate = "2024-01-01T00:00:00Z"

	// Test Info function
	info := Info()

	// Verify all expected fields are present
	assert.Contains(t, info, "Version: v1.2.3")
	assert.Contains(t, info, "Git Commit: abc123")
	assert.Contains(t, info, "Build Date: 2024-01-01T00:00:00Z")
	assert.Contains(t, info, "Go Version: "+runtime.Version())
	assert.Contains(t, info, "OS/Arch: "+runtime.GOOS+"/"+runtime.GOARCH)

	// Verify format
	lines := strings.Split(info, "\n")
	assert.Len(t, lines, 5)

	// Restore original values
	Version = origVersion
	GitCommit = origGitCommit
	BuildDate = origBuildDate
}

func TestShort(t *testing.T) {
	// Save original value
	origVersion := Version

	// Test with custom version
	Version = "v1.2.3"
	assert.Equal(t, "v1.2.3", Short())

	// Test with default version
	Version = "dev"
	assert.Equal(t, "dev", Short())

	// Restore original value
	Version = origVersion
}

func TestDefaultValues(t *testing.T) {
	assert.NotEmpty(t, Version)
	assert.NotEmpty(t, GitCommit)
	assert.NotEmpty(t, BuildDate)
}
