package cloud

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/sourcegraph/sourcegraph/dev/sg/internal/repo"
	"github.com/sourcegraph/sourcegraph/dev/sg/internal/std"
	"github.com/sourcegraph/sourcegraph/lib/errors"
)

var buildEphemeralCommand = cli.Command{
	Name:        "build",
	Usage:       "Trigger a cloud ephemeral build which will push images to the cloud ephemeral registry",
	Description: "Trigger a cloud ephemeral build which will push images to the cloud ephemeral registry",
	Action:      buildCloudEphemeral,
}

func writeAdditionalBuildCommandsSuggestion(version string) {
	versionLine := fmt.Sprintf("The build will push images with the following tag/version: `%s`", version)
	deployLine := fmt.Sprintf("create a deployment with this build by running `sg cloud deploy --version %s`", version)
	upgradeLine := fmt.Sprintf("upgrade the existing deployment with this build by running `sg cloud upgrade --version %s`", version)
	markdown := `%s
Once this build completes, you can:
* %s
* %s`
	std.Out.WriteMarkdown(withFAQ(fmt.Sprintf(markdown, versionLine, deployLine, upgradeLine)))
}

func buildCloudEphemeral(ctx *cli.Context) error {
	currentBranch, err := repo.GetCurrentBranch(ctx.Context)
	if err != nil {
		return errors.Wrap(err, "failed to determine current branch")
	}
	// We are on the branch we want to deploy, so we use the current commit
	head, err := repo.GetHeadCommit(ctx.Context)
	if err != nil {
		return errors.Wrap(err, "failed to determine current commit")
	}
	currRepo := repo.NewGitRepo(currentBranch, head)
	build, err := triggerEphemeralBuild(ctx.Context, currRepo)
	if err != nil {
		if errors.Is(err, ErrBranchOutOfSync) {
			std.Out.WriteWarningf(`Your branch %q is out of sync with the remote branch.

Please make sure you have either pushed or pulled the latest changes before trying again`, currRepo.Branch)
		} else if errors.Is(err, ErrMainBranchBuild) {
			std.Out.WriteWarningf(`Triggering Cloud Ephemeral builds from "main" is not supported.`)
			steps := "1. create a new branch off main by running `git switch <branch-name>`\n"
			steps += "2. push the branch to the remote by running `git push -u origin <branch-name>`\n"
			steps += "3. trigger the build by running `sg cloud ephemeral build`\n"
			std.Out.WriteMarkdown(withFAQ(fmt.Sprintf("Alternatively, if you still want to deploy \"main\" you can do:\n%s", steps)))
		}
		return errors.Wrapf(err, "failed to trigger epehemeral build for branch")
	}
	version, err := determineVersion(build, ctx.String("tag"))
	if err != nil {
		return err
	}
	writeAdditionalBuildCommandsSuggestion(version)
	return nil
}
