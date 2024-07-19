package graphqlbackend

import (
	"context"

	"github.com/graph-gophers/graphql-go"
)

type CodyContextResolver interface {
	GetCodyContext(ctx context.Context, args GetContextArgs) ([]ContextResultResolver, error)
	ChatIntent(ctx context.Context, args ChatIntentArgs) (IntentResolver, error)
	RankContext(ctx context.Context, args RankContextArgs) (RankContextResolver, error)
	RecordContext(ctx context.Context, args RecordContextArgs) (*EmptyResponse, error)
}

type GetContextArgs struct {
	Repos            []graphql.ID
	Query            string
	CodeResultsCount int32
	TextResultsCount int32
}

type ChatIntentArgs struct {
	Query         string
	InteractionID string
}

type RankContextArgs struct {
	Query                     string
	ContextItems              []InputContextItem
	RankOptions               *RankOptions
	TargetModel               *string
	TargetContextWindowTokens *int32
	Intent                    *string
	Command                   *string
	InteractionID             string
}

type RecordContextArgs struct {
	InteractionID       string
	UsedContextItems    []InputContextItem
	IgnoredContextItems []InputContextItem
}

type InputContextItem struct {
	Content   string
	Retriever string
	Score     *float64
	FileName  *string
	StartLine *int32
	EndLine   *int32
}

type RankOptions struct {
	Ranker string
}

type IntentResolver interface {
	Intent() string
	Score() float64
}

type ContextResultResolver interface {
	ToFileChunkContext() (*FileChunkContextResolver, bool)
}

func NewFileChunkContextResolver(gitTreeEntryResolver *GitTreeEntryResolver, startLine, endLine int) *FileChunkContextResolver {
	return &FileChunkContextResolver{
		treeEntry: gitTreeEntryResolver,
		startLine: int32(startLine),
		endLine:   int32(endLine),
	}
}

type FileChunkContextResolver struct {
	treeEntry          *GitTreeEntryResolver
	startLine, endLine int32
}

var _ ContextResultResolver = (*FileChunkContextResolver)(nil)

func (f *FileChunkContextResolver) Blob() *GitTreeEntryResolver { return f.treeEntry }
func (f *FileChunkContextResolver) StartLine() int32            { return f.startLine }
func (f *FileChunkContextResolver) EndLine() int32              { return f.endLine }
func (f *FileChunkContextResolver) ToFileChunkContext() (*FileChunkContextResolver, bool) {
	return f, true
}

func (f *FileChunkContextResolver) ChunkContent(ctx context.Context) (string, error) {
	return f.treeEntry.Content(ctx, &GitTreeContentPageArgs{
		StartLine: &f.startLine,
		EndLine:   &f.endLine,
	})
}

type RankContextResolver interface {
	Ranker() string
	Used() []int32
	Ignored() []int32
}
