package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubSettings implements SettingsReader with a fixed map.
type stubSettings struct {
	values map[string]string
	err    error
}

func (s stubSettings) GetAll(context.Context, string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.values, nil
}

func TestResolveProjectForCWD_LongestPrefixWins(t *testing.T) {
	svc := NewService(ServiceConfig{
		SettingsService: stubSettings{values: map[string]string{
			RepoProjectMapSetting: `{"/Users/x/repos/hegemon":"arctop","/Users/x/repos/hegemon/firmware/mw75neuro":"mw75-neuro-firmware","/Users/x/repos/seam":"seam"}`,
		}},
	})
	ctx := context.Background()

	require.Equal(t, "seam", svc.ResolveProjectForCWD(ctx, testUserID, "/Users/x/repos/seam"))
	require.Equal(t, "seam", svc.ResolveProjectForCWD(ctx, testUserID, "/Users/x/repos/seam/internal/agent"))
	// Umbrella vs more-specific: longest prefix wins.
	require.Equal(t, "arctop", svc.ResolveProjectForCWD(ctx, testUserID, "/Users/x/repos/hegemon/other"))
	require.Equal(t, "mw75-neuro-firmware", svc.ResolveProjectForCWD(ctx, testUserID, "/Users/x/repos/hegemon/firmware/mw75neuro/src"))
}

func TestResolveProjectForCWD_BoundaryNotSubstring(t *testing.T) {
	svc := NewService(ServiceConfig{
		SettingsService: stubSettings{values: map[string]string{
			RepoProjectMapSetting: `{"/a/b":"proj"}`,
		}},
	})
	ctx := context.Background()

	// "/a/b" must NOT match "/a/bc" (substring but not a path prefix).
	require.Equal(t, "", svc.ResolveProjectForCWD(ctx, testUserID, "/a/bc"))
	require.Equal(t, "proj", svc.ResolveProjectForCWD(ctx, testUserID, "/a/b"))
	require.Equal(t, "proj", svc.ResolveProjectForCWD(ctx, testUserID, "/a/b/c"))
}

func TestResolveProjectForCWD_FailureSoft(t *testing.T) {
	ctx := context.Background()

	// No settings service.
	svc := NewService(ServiceConfig{})
	require.Equal(t, "", svc.ResolveProjectForCWD(ctx, testUserID, "/a/b"))

	// Empty cwd.
	svc2 := NewService(ServiceConfig{SettingsService: stubSettings{values: map[string]string{RepoProjectMapSetting: `{"/a":"p"}`}}})
	require.Equal(t, "", svc2.ResolveProjectForCWD(ctx, testUserID, ""))

	// Empty / default map.
	svc3 := NewService(ServiceConfig{SettingsService: stubSettings{values: map[string]string{RepoProjectMapSetting: "{}"}}})
	require.Equal(t, "", svc3.ResolveProjectForCWD(ctx, testUserID, "/a/b"))

	// Malformed JSON -> "".
	svc4 := NewService(ServiceConfig{SettingsService: stubSettings{values: map[string]string{RepoProjectMapSetting: "not json"}}})
	require.Equal(t, "", svc4.ResolveProjectForCWD(ctx, testUserID, "/a/b"))
}
