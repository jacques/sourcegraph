package job_store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/keegancsmith/sqlf"
	"github.com/sourcegraph/log"
	"github.com/sourcegraph/sourcegraph/internal/actor"
	"github.com/sourcegraph/sourcegraph/internal/authz"
	"github.com/sourcegraph/sourcegraph/internal/conf"
	"github.com/sourcegraph/sourcegraph/internal/conf/conftypes"
	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	connections "github.com/sourcegraph/sourcegraph/internal/database/connections/live"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	dbworkerstore "github.com/sourcegraph/sourcegraph/internal/workerutil/dbworker/store"
)

type SyntacticIndexingJobStore interface {
	DBWorkerStore() dbworkerstore.Store[*SyntacticIndexingJob]
	InsertIndexes(ctx context.Context, indexes []SyntacticIndexingJob) ([]SyntacticIndexingJob, error)
	IsQueued(ctx context.Context, repositoryID int, commit string) (bool, error)
}

type syntacticIndexingJobStoreImpl struct {
	store dbworkerstore.Store[*SyntacticIndexingJob]
	db    *basestore.Store
}

var _ SyntacticIndexingJobStore = &syntacticIndexingJobStoreImpl{}

func initDB(observationCtx *observation.Context, name string) *sql.DB {
	// This is an internal service, so we rely on the
	// frontend to do authz checks for user requests.
	// Authz checks are enforced by the DB layer
	//
	// This call to SetProviders is here so that calls to GetProviders don't block.
	// Relevant PR: https://github.com/sourcegraph/sourcegraph/pull/15755
	// Relevant issue: https://github.com/sourcegraph/sourcegraph/issues/15962

	authz.SetProviders(true, []authz.Provider{})

	dsn := conf.GetServiceConnectionValueAndRestartOnChange(func(serviceConnections conftypes.ServiceConnections) string {
		return serviceConnections.PostgresDSN
	})

	sqlDB, err := connections.EnsureNewFrontendDB(observationCtx, dsn, name)

	if err != nil {
		log.Scoped("init db ("+name+")").Fatal("Failed to connect to frontend database", log.Error(err))
	}

	return sqlDB
}

func NewStore(observationCtx *observation.Context, name string) (SyntacticIndexingJobStore, error) {
	db := initDB(observationCtx, name)

	return NewStoreWithDB(observationCtx, db)
}

func NewStoreWithDB(observationCtx *observation.Context, db *sql.DB) (SyntacticIndexingJobStore, error) {

	// Make sure this is in sync with the columns of the
	// syntactic_scip_indexing_jobs_with_repository_name view
	var columnExpressions = []*sqlf.Query{
		sqlf.Sprintf("u.id"),
		sqlf.Sprintf("u.commit"),
		sqlf.Sprintf("u.queued_at"),
		sqlf.Sprintf("u.state"),
		sqlf.Sprintf("u.failure_message"),
		sqlf.Sprintf("u.started_at"),
		sqlf.Sprintf("u.finished_at"),
		sqlf.Sprintf("u.process_after"),
		sqlf.Sprintf("u.num_resets"),
		sqlf.Sprintf("u.num_failures"),
		sqlf.Sprintf("u.repository_id"),
		sqlf.Sprintf("u.repository_name"),
		sqlf.Sprintf("u.should_reindex"),
		sqlf.Sprintf("u.enqueuer_user_id"),
	}

	storeOptions := dbworkerstore.Options[*SyntacticIndexingJob]{
		Name:      "syntactic_scip_indexing_jobs_store",
		TableName: "syntactic_scip_indexing_jobs",
		ViewName:  "syntactic_scip_indexing_jobs_with_repository_name u",
		// Using enqueuer_user_id prioritises manually scheduled indexing
		OrderByExpression: sqlf.Sprintf("(u.enqueuer_user_id > 0) DESC, u.queued_at, u.id"),
		ColumnExpressions: columnExpressions,
		Scan:              dbworkerstore.BuildWorkerScan(ScanSyntacticIndexRecord),
	}

	handle := basestore.NewHandleWithDB(observationCtx.Logger, db, sql.TxOptions{})
	return &syntacticIndexingJobStoreImpl{
		store: dbworkerstore.New(observationCtx, handle, storeOptions),
		db:    basestore.NewWithHandle(handle),
	}, nil
}

func (s *syntacticIndexingJobStoreImpl) InsertIndexes(ctx context.Context, indexes []SyntacticIndexingJob) ([]SyntacticIndexingJob, error) {

	// ctx, _, endObservation := s.operations.insertIndexes.With(ctx, &err, observation.Args{Attrs: []attribute.KeyValue{
	// 	attribute.Int("numIndexes", len(indexes)),
	// }})
	// endObservation(1, observation.Args{})

	if len(indexes) == 0 {
		return nil, nil
	}

	actor := actor.FromContext(ctx)

	values := make([]*sqlf.Query, 0, len(indexes))
	for _, index := range indexes {
		values = append(values, sqlf.Sprintf(
			"(%s, %s, %s, %s)",
			index.State,
			index.Commit,
			index.RepositoryID,
			actor.UID,
		))
	}

	indexes = []SyntacticIndexingJob{}
	err := s.db.WithTransact(ctx, func(tx *basestore.Store) error {
		ids, err := basestore.ScanInts(tx.Query(ctx, sqlf.Sprintf(insertIndexQuery, sqlf.Join(values, ","))))
		if err != nil {
			return err
		}

		fmt.Println("Inserting indices", ids)

		return nil

		// TODO: hydrate the list of indexes by retrieving them

		// s.operations.indexesInserted.Add(float64(len(ids)))

		// authzConds, err := database.AuthzQueryConds(ctx, database.NewDBWith(s.logger, s.db))
		// if err != nil {
		// 	return err
		// }

		// queries := make([]*sqlf.Query, 0, len(ids))
		// for _, id := range ids {
		// 	queries = append(queries, sqlf.Sprintf("%d", id))
		// }

		// indexes, err = scanIndexes(tx.db.Query(ctx, sqlf.Sprintf(getIndexesByIDsQuery, sqlf.Join(queries, ", "), authzConds)))
		// return err
	})

	return indexes, err
}

func (s *syntacticIndexingJobStoreImpl) IsQueued(ctx context.Context, repositoryID int, commit string) (bool, error) {

	isQueued, _, err := basestore.ScanFirstBool(s.db.Query(ctx, sqlf.Sprintf(
		isQueuedQuery,
		repositoryID,
		commit,
	)))

	return isQueued, err
}

const insertIndexQuery = `
INSERT INTO syntactic_scip_indexing_jobs (
	state,
	commit,
	repository_id,
	enqueuer_user_id
)
VALUES %s
RETURNING id
`

const isQueuedQuery = `
SELECT EXISTS(
	SELECT queued_at
	FROM syntactic_scip_indexing_jobs
	WHERE
		repository_id  = %s AND
		commit = %s
	ORDER BY queued_at DESC
	LIMIT 1
)
`

func (s *syntacticIndexingJobStoreImpl) DBWorkerStore() dbworkerstore.Store[*SyntacticIndexingJob] {
	return s.store
}
