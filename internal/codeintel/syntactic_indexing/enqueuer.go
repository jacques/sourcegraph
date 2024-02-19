package syntactic_indexing

import (
	"context"
	"fmt"

	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/codeintel/reposcheduler"
	"github.com/sourcegraph/sourcegraph/internal/codeintel/syntactic_indexing/job_store"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/gitserver"
	"github.com/sourcegraph/sourcegraph/internal/metrics"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/lib/errors"
	"go.opentelemetry.io/otel/attribute"
)

type IndexEnqueuer interface {
	QueueIndexes(ctx context.Context, repositoryId int, rev, configuration string, options EnqueueOptions) (_ []job_store.SyntacticIndexingJob, err error)
}

type EnqueueOptions struct {
	force       bool
	bypassLimit bool
}

type indexEnqueuerImpl struct {
	jobStore            job_store.SyntacticIndexingJobStore
	repoSchedulingStore reposcheduler.RepositorySchedulingStore
	repoStore           database.RepoStore
	gitserverClient     gitserver.Client
	operations          *operations
}

var _ IndexEnqueuer = &indexEnqueuerImpl{}

func NewIndexEnqueuer(
	observationCtx *observation.Context,
	jobStore job_store.SyntacticIndexingJobStore,
	store reposcheduler.RepositorySchedulingStore,
	repoStore database.RepoStore,
	gitserverClient gitserver.Client,
) IndexEnqueuer {
	return &indexEnqueuerImpl{
		repoSchedulingStore: store,
		repoStore:           repoStore,
		gitserverClient:     gitserverClient,
		jobStore:            jobStore,
		operations:          newOperations(observationCtx),
	}
}

type operations struct {
	queueIndex *observation.Operation
}

var (
	m = new(metrics.SingletonREDMetrics)
)

func newOperations(observationCtx *observation.Context) *operations {
	m := m.Get(func() *metrics.REDMetrics {
		return metrics.NewREDMetrics(
			observationCtx.Registerer,
			"codeintel_autoindexing_store",
			metrics.WithLabels("op"),
			metrics.WithCountHelp("Total number of method invocations."),
		)
	})

	op := func(name string) *observation.Operation {
		return observationCtx.Operation(observation.Op{
			Name:              fmt.Sprintf("codeintel.syntactic_indexing.enqueuer.%s", name),
			MetricLabelValues: []string{name},
			Metrics:           m,
		})
	}

	return &operations{
		queueIndex: op("QueueIndex"),
	}
}

// QueueIndexes enqueues a set of index jobs for the following repository and commit. If a non-empty
// configuration is given, it will be used to determine the set of jobs to enqueue. Otherwise, it will
// the configuration will be determined based on the regular index scheduling rules: first read any
// in-repo configuration (e.g., sourcegraph.yaml), then look for any existing in-database configuration,
// finally falling back to the automatically inferred configuration based on the repo contents at the
// target commit.
//
// If the force flag is false, then the presence of an upload or index record for this given repository and commit
// will cause this method to no-op. Note that this is NOT a guarantee that there will never be any duplicate records
// when the flag is false.IsQueued
func (s *indexEnqueuerImpl) QueueIndexes(ctx context.Context, repositoryID int, rev, configuration string, options EnqueueOptions) (_ []job_store.SyntacticIndexingJob, err error) {
	ctx, trace, endObservation := s.operations.queueIndex.With(ctx, &err, observation.Args{Attrs: []attribute.KeyValue{
		attribute.Int("repositoryID", repositoryID),
		attribute.String("rev", rev),
	}})
	defer endObservation(1, observation.Args{})

	repo, err := s.repoStore.Get(ctx, api.RepoID(repositoryID))
	if err != nil {
		return nil, err
	}

	commitID, err := s.gitserverClient.ResolveRevision(ctx, repo.Name, rev, gitserver.ResolveRevisionOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "gitserver.ResolveRevision")
	}
	commit := string(commitID)
	trace.AddEvent("ResolveRevision", attribute.String("commit", commit))

	return s.queueIndexForRepositoryAndCommit(ctx, repositoryID, commit, configuration, options)
}

// queueIndexForRepositoryAndCommit determines a set of index jobs to enqueue for the given repository and commit.
//
// If the force flag is false, then the presence of an upload or index record for this given repository and commit
// will cause this method to no-op. Note that this is NOT a guarantee that there will never be any duplicate records
// when the flag is false.
func (s *indexEnqueuerImpl) queueIndexForRepositoryAndCommit(ctx context.Context, repositoryID int, commit, configuration string, options EnqueueOptions) ([]job_store.SyntacticIndexingJob, error) {
	if !options.force {
		isQueued, err := s.jobStore.IsQueued(ctx, repositoryID, commit)
		fmt.Println("Queueing", repositoryID, commit, isQueued)
		if err != nil {
			return nil, errors.Wrap(err, "dbstore.IsQueued")
		}
		if isQueued {
			return nil, nil
		}
	}

	if !options.force {
		values := make([]job_store.SyntacticIndexingJob, 1)
		values[0] = job_store.SyntacticIndexingJob{
			State:        job_store.Queued,
			Commit:       commit,
			RepositoryID: repositoryID,
		}

		_, err := s.jobStore.InsertIndexes(ctx, values)
		if err != nil {
			return nil, err
		}
	}

	// indexes, err := s.jobSelector.GetIndexRecords(ctx, repositoryID, commit, configuration, bypassLimit)
	// if err != nil {
	// 	return nil, err
	// }
	// if len(indexes) == 0 {
	// 	return nil, nil
	// }

	// indexesToInsert := indexes
	// if !force {
	// 	indexesToInsert = []uploadsshared.Index{}
	// 	for _, index := range indexes {
	// 		isQueued, err := s.store.IsQueuedRootIndexer(ctx, repositoryID, commit, index.Root, index.Indexer)
	// 		if err != nil {
	// 			return nil, errors.Wrap(err, "dbstore.IsQueuedRootIndexer")
	// 		}
	// 		if !isQueued {
	// 			indexesToInsert = append(indexesToInsert, index)
	// 		}
	// 	}
	// }

	// return s.store.InsertIndexes(ctx, indexesToInsert)

	return nil, nil
}
