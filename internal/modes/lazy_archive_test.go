package modes

import (
	"testing"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/env"
)

func TestLazyArchiveRetainsProviderConfigurationFailure(t *testing.T) {
	failure := anna.NewError(anna.CodeConfig, "invalid archive setting")
	archive := &lazyArchive{config: &env.Env{ArchiveError: failure}}
	first, err := archive.get()
	if first != nil || err != failure {
		t.Fatal("archive validation error was not retained")
	}
	second, repeated := archive.get()
	if second != nil || repeated != failure {
		t.Fatal("repeated archive initialization changed its failure")
	}
}

func TestLazyArchiveConstructsClientWithoutCredentialsOrNetwork(t *testing.T) {
	archive := &lazyArchive{config: &env.Env{AnnasBaseURL: env.DefaultAnnasBaseURL}}
	first, err := archive.get()
	if err != nil || first == nil {
		t.Fatalf("lazy client construction failed: %v", err)
	}
	second, err := archive.get()
	if err != nil || second != first {
		t.Fatal("lazy archive constructed more than one SDK client")
	}
}

func TestPaperDetailsRejectsOverflowPageBeforeProviderAccess(t *testing.T) {
	_, err := (&service{}).paperDetails(t.Context(), ArticleDetailsParams{ID: "W1", LocationsPage: int(^uint(0) >> 1)}, 0)
	if anna.CodeOf(err) != anna.InvalidArgument {
		t.Fatalf("overflowing locations page must fail locally: %v", err)
	}
}
