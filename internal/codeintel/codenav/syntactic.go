package codenav

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"slices"

	genslices "github.com/life4/genesis/slices"
	"github.com/sourcegraph/log"
	"github.com/sourcegraph/scip/bindings/go/scip"
	orderedmap "github.com/wk8/go-ordered-map/v2"
	"go.opentelemetry.io/otel/attribute"

	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/codeintel/core"
	"github.com/sourcegraph/sourcegraph/internal/collections"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/internal/search"
	searchclient "github.com/sourcegraph/sourcegraph/internal/search/client"
	"github.com/sourcegraph/sourcegraph/internal/search/result"
	"github.com/sourcegraph/sourcegraph/internal/search/streaming"
	"github.com/sourcegraph/sourcegraph/lib/errors"
)

type candidateFile struct {
	matches             []scip.Range // Guaranteed to be sorted
	didSearchEntireFile bool         // Or did we hit the search count limit?
}

type searchArgs struct {
	repo       api.RepoName
	commit     api.CommitID
	identifier string
	language   string
}

// findCandidateOccurrencesViaSearch calls out to Searcher/Zoekt to find candidate occurrences of the given symbol.
// It returns a map of file paths to candidate ranges.
func findCandidateOccurrencesViaSearch(
	ctx context.Context,
	trace observation.TraceLogger,
	client searchclient.SearchClient,
	args searchArgs,
) (orderedmap.OrderedMap[core.RepoRelPath, candidateFile], error) {
	if args.identifier == "" {
		return *orderedmap.New[core.RepoRelPath, candidateFile](), nil
	}
	resultMap := *orderedmap.New[core.RepoRelPath, candidateFile]()
	// TODO: countLimit should be dependent on the number of requested usages, with a configured global limit
	// For now we're matching the current web app with 500
	searchResults, err := executeQuery(ctx, client, trace, args, "file", 500, 0)
	if err != nil {
		return resultMap, err
	}

	nonFileMatches := 0
	inconsistentFilepaths := 0
	duplicatedFilepaths := collections.NewSet[string]()
	matchCount := 0
	for _, streamResult := range searchResults {
		fileMatch, ok := streamResult.(*result.FileMatch)
		if !ok {
			nonFileMatches += 1
			continue
		}
		path := fileMatch.Path
		matches := []scip.Range{}
		for _, chunkMatch := range fileMatch.ChunkMatches {
			for _, matchRange := range chunkMatch.Ranges {
				if path != streamResult.Key().Path {
					inconsistentFilepaths = 1
					continue
				}
				scipRange, err := scipFromResultRange(matchRange)
				if err != nil {
					trace.Warn("Failed to create scip range from match range",
						log.String("error", err.Error()),
						log.String("matchRange", fmt.Sprintf("%+v", matchRange)),
					)
					continue
				}
				matchCount += 1
				matches = append(matches, scipRange)
			}
		}
		// OK to use Unchecked method here as search API only returns repo-root relative paths
		_, alreadyPresent := resultMap.Set(core.NewRepoRelPathUnchecked(path), candidateFile{
			matches:             scip.SortRanges(matches),
			didSearchEntireFile: !fileMatch.LimitHit,
		})
		if alreadyPresent {
			duplicatedFilepaths.Add(path)
		}
	}
	trace.AddEvent("findCandidateOccurrencesViaSearch", attribute.Int("matchCount", matchCount))

	if !duplicatedFilepaths.IsEmpty() {
		trace.Warn("Saw duplicate file paths in search results", log.String("paths", duplicatedFilepaths.String()))
	}
	if nonFileMatches != 0 {
		trace.Warn("Saw non file match in search results. The `type:file` on the query should guarantee this")
	}
	if inconsistentFilepaths != 0 {
		trace.Warn("Saw mismatched file paths between chunk matches in the same FileMatch. Report this to the search-platform")
	}

	return resultMap, nil
}

type symbolData struct {
	range_ scip.Range
	kind   string
}

func (s *symbolData) Range() scip.Range {
	return s.range_
}

// symbolSearchResult maps file paths to a list of symbols sorted by range
type symbolSearchResult struct {
	inner orderedmap.OrderedMap[core.RepoRelPath, []symbolData]
}

func (s *symbolSearchResult) Contains(path core.RepoRelPath, range_ scip.Range) bool {
	if symbols, ok := s.inner.Get(path); ok {
		_, found := slices.BinarySearchFunc(symbols, range_, func(s1 symbolData, s2 scip.Range) int {
			return s1.range_.CompareStrict(s2)
		})
		return found
	}
	return false
}

func symbolSearch(
	ctx context.Context,
	trace observation.TraceLogger,
	client searchclient.SearchClient,
	args searchArgs,
) (symbolSearchResult, error) {
	if args.identifier == "" {
		return symbolSearchResult{}, nil
	}
	// Using the same limit as the current web app
	searchResults, err := executeQuery(ctx, client, trace, args, "symbol", 50, 0)
	if err != nil {
		return symbolSearchResult{}, err
	}

	matchCount := 0
	resultMap := *orderedmap.New[core.RepoRelPath, []symbolData]()
	for _, streamResult := range searchResults {
		fileMatch, ok := streamResult.(*result.FileMatch)
		if !ok {
			continue
		}
		symbolDatas := genslices.MapFilter(fileMatch.Symbols, func(symbol *result.SymbolMatch) (symbolData, bool) {
			scipRange, err := scip.NewRange([]int32{
				int32(symbol.Symbol.Range().Start.Line),
				int32(symbol.Symbol.Range().Start.Character),
				int32(symbol.Symbol.Range().End.Line),
				int32(symbol.Symbol.Range().End.Character),
			})
			if err != nil {
				return symbolData{}, false
			}
			return symbolData{
				range_: scipRange,
				kind:   symbol.Symbol.Kind,
			}, true
		})
		slices.SortFunc(symbolDatas, func(s1 symbolData, s2 symbolData) int {
			return s1.range_.CompareStrict(s2.range_)
		})
		matchCount += len(symbolDatas)
		resultMap.Set(core.NewRepoRelPathUnchecked(fileMatch.Path), symbolDatas)
	}
	trace.AddEvent("symbolSearch", attribute.Int("matchCount", matchCount))

	return symbolSearchResult{resultMap}, nil
}

func buildQuery(args searchArgs, queryType string, countLimit int) string {
	repoName := fmt.Sprintf("^%s$", args.repo)
	wordBoundaryIdentifier := fmt.Sprintf("/\\b%s\\b/", args.identifier)
	return fmt.Sprintf(
		"case:yes type:%s repo:%s rev:%s language:%s count:%d %s",
		queryType, repoName, string(args.commit), args.language, countLimit, wordBoundaryIdentifier)
}

func executeQuery(
	ctx context.Context,
	client searchclient.SearchClient,
	trace observation.TraceLogger,
	args searchArgs,
	queryType string,
	countLimit int,
	surroundingLines int,
) (result.Matches, error) {
	searchQuery := buildQuery(args, queryType, countLimit)
	patternType := "standard"
	contextLines := int32(surroundingLines)
	plan, err := client.Plan(ctx, "V3", &patternType, searchQuery, search.Precise, search.Streaming, &contextLines)
	if err != nil {
		return nil, err
	}
	trace.Info("Running query", log.String("query", searchQuery))
	stream := streaming.NewAggregatingStream()
	_, err = client.Execute(ctx, stream, plan)
	if err != nil {
		return nil, err
	}
	return stream.Results, nil
}

func nameFromGlobalSymbol(symbol *scip.Symbol) (string, bool) {
	if len(symbol.Descriptors) == 0 || symbol.Descriptors[0].Suffix == scip.Descriptor_Local {
		return "", false
	}
	return symbol.Descriptors[len(symbol.Descriptors)-1].Name, true
}

// sliceRangeFromReader returns the substring corresponding to the given single-line range.
// It fails if the range spans multiple lines or it is out-of-bounds for the reader
func sliceRangeFromReader(reader io.Reader, range_ scip.Range) (substr string, err error) {
	if range_.Start.Line != range_.End.Line {
		return "", errors.New("symbol range spans multiple lines")
	}

	scanner := bufio.NewScanner(reader)
	for i := int32(0); scanner.Scan() && i <= range_.Start.Line; i++ {
		if i == range_.Start.Line {
			line := scanner.Text()
			if len(line) < int(range_.End.Character) {
				return "", errors.New("symbol range is out-of-bounds")
			}
			// FIXME(issue: GRAPH-715): wrong (less wrong would be to use rune offsets, actually correct needs encoding of the string _and_ the scip.Range)
			return line[range_.Start.Character:range_.End.Character], nil
		}
	}
	return "", errors.New("symbol range is out-of-bounds")
}

func scipFromResultRange(resultRange result.Range) (scip.Range, error) {
	return scip.NewRange([]int32{
		int32(resultRange.Start.Line),
		int32(resultRange.Start.Column),
		int32(resultRange.End.Line),
		int32(resultRange.End.Column),
	})
}

// symbolAtRange tries to look up the symbols at the given coordinates
// in a syntactic upload. If this function returns an error you should most likely
// log and handle it instead of rethrowing, as this could fail for a myriad of reasons
// (some broken invariant internally, network issue etc.)
// If this function doesn't error, the returned slice is guaranteed to be non-empty
func symbolAtRange(
	ctx context.Context,
	mappedIndex MappedIndex,
	args UsagesForSymbolArgs,
) (*scip.Symbol, error) {
	docOpt, err := mappedIndex.GetDocument(ctx, args.Path)
	if err != nil {
		return nil, err
	}
	doc, isSome := docOpt.Get()
	if !isSome {
		return nil, errors.New("no document found")
	}
	occs, err := doc.GetOccurrencesAtRange(ctx, args.SymbolRange)
	if err != nil {
		return nil, err
	}
	if len(occs) == 0 {
		return nil, errors.New("no occurrences found")
	}
	sym, err := scip.ParseSymbol(occs[0].Symbol)
	if err != nil {
		return nil, err
	}
	return sym, nil
}

func findSyntacticMatchesForCandidateFile(
	ctx context.Context,
	trace observation.TraceLogger,
	mappedIndex MappedIndex,
	filePath core.RepoRelPath,
	candidateFile candidateFile,
) ([]SyntacticMatch, []SearchBasedMatch, *SyntacticUsagesError) {
	documentOpt, docErr := mappedIndex.GetDocument(ctx, filePath)
	if docErr != nil {
		return nil, nil, &SyntacticUsagesError{
			Code:            SU_Fatal,
			UnderlyingError: docErr,
		}
	}
	document, isSome := documentOpt.Get()
	if !isSome {
		return nil, nil, &SyntacticUsagesError{
			Code: SU_NoSyntacticIndex,
		}
	}
	syntacticMatches := []SyntacticMatch{}
	searchBasedMatches := []SearchBasedMatch{}

	failedTranslationCount := 0
	for _, sourceCandidateRange := range candidateFile.matches {
		foundSyntacticMatch := false
		occurrences, occErr := document.GetOccurrencesAtRange(ctx, sourceCandidateRange)
		if occErr != nil {
			failedTranslationCount += 1
			continue
		}
		for _, occ := range occurrences {
			if !scip.IsLocalSymbol(occ.Symbol) {
				foundSyntacticMatch = true
				syntacticMatches = append(syntacticMatches, SyntacticMatch{
					Path:         filePath,
					Range:        sourceCandidateRange,
					Symbol:       occ.Symbol,
					IsDefinition: scip.SymbolRole_Definition.Matches(occ),
				})
			}
		}
		if !foundSyntacticMatch {
			searchBasedMatches = append(searchBasedMatches, SearchBasedMatch{
				Path:  filePath,
				Range: sourceCandidateRange,
			})
		}
	}
	if failedTranslationCount != 0 {
		trace.Info("findSyntacticMatchesForCandidateFile", log.Int("failedTranslationCount", failedTranslationCount))
	}
	return syntacticMatches, searchBasedMatches, nil
}

func syntacticUsagesImpl(
	ctx context.Context,
	trace observation.TraceLogger,
	searchClient searchclient.SearchClient,
	mappedIndex MappedIndex,
	args UsagesForSymbolArgs,
) (SyntacticUsagesResult, PreviousSyntacticSearch, *SyntacticUsagesError) {
	searchSymbol, symErr := symbolAtRange(ctx, mappedIndex, args)
	if symErr != nil {
		return SyntacticUsagesResult{}, PreviousSyntacticSearch{}, &SyntacticUsagesError{
			Code:            SU_NoSymbolAtRequestedRange,
			UnderlyingError: symErr,
		}
	}
	language, langErr := languageFromFilepath(trace, args.Path)
	if langErr != nil {
		return SyntacticUsagesResult{}, PreviousSyntacticSearch{}, &SyntacticUsagesError{
			Code:            SU_FailedToSearch,
			UnderlyingError: langErr,
		}
	}
	symbolName, ok := nameFromGlobalSymbol(searchSymbol)
	if !ok {
		return SyntacticUsagesResult{}, PreviousSyntacticSearch{}, &SyntacticUsagesError{
			Code:            SU_FailedToSearch,
			UnderlyingError: errors.New("can't find syntactic occurrences for locals via search"),
		}
	}
	searchCoords := searchArgs{
		repo:       args.Repo.Name,
		commit:     args.Commit,
		identifier: symbolName,
		language:   language,
	}
	candidateMatches, searchErr := findCandidateOccurrencesViaSearch(ctx, trace, searchClient, searchCoords)
	if searchErr != nil {
		return SyntacticUsagesResult{}, PreviousSyntacticSearch{}, &SyntacticUsagesError{
			Code:            SU_FailedToSearch,
			UnderlyingError: searchErr,
		}
	}

	results := [][]SyntacticMatch{}

	for pair := candidateMatches.Oldest(); pair != nil; pair = pair.Next() {
		// We're assuming the upload we found earlier contains the relevant SCIP document
		// see NOTE(id: single-syntactic-upload)
		syntacticMatches, _, err := findSyntacticMatchesForCandidateFile(ctx, trace, mappedIndex, pair.Key, pair.Value)
		if err != nil {
			// TODO: Errors that are not "no index found in the DB" should be reported
			// TODO: Track metrics about how often this happens (GRAPH-693)
			continue
		}
		results = append(results, syntacticMatches)
	}
	return SyntacticUsagesResult{Matches: slices.Concat(results...)}, PreviousSyntacticSearch{
		MappedIndex: mappedIndex,
		SymbolName:  symbolName,
		Language:    language,
	}, nil
}

// searchBasedUsagesImpl is extracted from SearchBasedUsages to allow
// testing of the core logic, by only mocking the search client.
func searchBasedUsagesImpl(
	ctx context.Context,
	trace observation.TraceLogger,
	searchClient searchclient.SearchClient,
	args UsagesForSymbolArgs,
	symbolName string,
	language string,
	syntacticIndex core.Option[MappedIndex],
) (matches []SearchBasedMatch, err error) {
	searchCoords := searchArgs{
		repo:       args.Repo.Name,
		commit:     args.Commit,
		identifier: symbolName,
		language:   language,
	}
	candidateMatches, err := findCandidateOccurrencesViaSearch(ctx, trace, searchClient, searchCoords)
	if err != nil {
		return nil, err
	}
	candidateSymbols, err := symbolSearch(ctx, trace, searchClient, searchCoords)
	if err != nil {
		trace.Warn("Failed to run symbol search, will not mark any search-based usages as definitions", log.Error(err))
	}

	results := [][]SearchBasedMatch{}
	for pair := candidateMatches.Oldest(); pair != nil; pair = pair.Next() {
		if index, ok := syntacticIndex.Get(); ok {
			_, searchBasedMatches, err := findSyntacticMatchesForCandidateFile(ctx, trace, index, pair.Key, pair.Value)
			if err == nil {
				results = append(results, searchBasedMatches)
				continue
			} else {
				trace.Info("findSyntacticMatches failed, skipping filtering search-based results", log.Error(err))
			}
		}
		matches := []SearchBasedMatch{}
		for _, rg := range pair.Value.matches {
			matches = append(matches, SearchBasedMatch{
				Path:         pair.Key,
				Range:        rg,
				IsDefinition: candidateSymbols.Contains(pair.Key, rg),
			})
		}
		results = append(results, matches)
	}
	return slices.Concat(results...), nil
}
