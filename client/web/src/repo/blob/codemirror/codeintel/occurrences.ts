import { Facet } from '@codemirror/state'

import { Occurrence, Position, nonOverlappingOccurrences } from '@sourcegraph/shared/src/codeintel/scip'

// CodeGraphData conforms to the CodeGraphData GraphQL type, but
// we can't reference that type from here since it is generated by
// Svelte-specific build tooling.
export interface CodeGraphData {
    provenance: Provenance
    commit: string
    toolInfo: {
        name: string | null
        version: string | null
    } | null
    // The raw occurrences as returned by the API. Guaranteed to be sorted.
    occurrences: Occurrence[]
}

export type Provenance = 'PRECISE' | 'SYNTACTIC' | 'SEARCH_BASED'

// IndexedCodeGraphData adds an occurrence index to the code graph
// data, which guarantees things like non-overlapping ranges, sorted
// ranges, and a line index for faster lookups.
export interface IndexedCodeGraphData extends CodeGraphData {
    occurrenceIndex: OccurrenceIndex
}

// This data structure combines the occurrences data received from the
// server with a lineIndex map (implemented as array), for fast lookup by line
// number, with minimal additional impact on memory (e.g. garbage collection).
// Additionally, it flattens the list of occurrences so they are non-overlapping.
export class OccurrenceIndex extends Array<Occurrence> {
    public lineIndex: (number | undefined)[]

    constructor(occurrences: readonly Occurrence[]) {
        const lineIndex: (number | undefined)[] = []
        let previousEndline: number | undefined

        for (let index = 0; index < occurrences.length; index++) {
            const current = occurrences[index]

            if (previousEndline !== current.range.start.line) {
                // Only use the current index if there isn't already an occurrence on
                // the current line.
                lineIndex[current.range.start.line] = index
            }

            if (!current.range.isSingleLine()) {
                lineIndex[current.range.end.line] = index
            }

            previousEndline = current.range.end.line
        }

        // CAUTION: Do not "optimize" this to super(...nonOverlappingOccurrences(occurrences))
        // as Chrome will push all elements to a stack, and potentially trigger a stack overflow.
        // Similar bug in Nodejs: https://github.com/nodejs/node/issues/16870
        super()
        for (const occ of nonOverlappingOccurrences(occurrences)) {
            this.push(occ)
        }
        this.lineIndex = lineIndex
    }

    // atPosition returns the occurrence whose range contains position,
    // or undefined if no such occurrence exists.
    public atPosition(position: Position): Occurrence | undefined {
        // Binary search over the sorted, non-overlapping ranges.
        let [low, high] = [0, this.length]
        while (low < high) {
            const mid = Math.floor((low + high) / 2)
            if (this[mid].range.contains(position)) {
                return this[mid]
            }
            if (this[mid].range.end.compare(position) < 0) {
                low = mid + 1
            } else {
                high = mid
            }
        }
        return undefined
    }

    public next(
        from: Position,
        step: 'line' | 'character',
        direction: 'next' | 'previous' = 'next'
    ): Occurrence | undefined {
        let occurrence
        if (step === 'character') {
            occurrence = this.closestByCharacter(
                from.line,
                from,
                direction === 'next'
                    ? occurrence => occurrence.range.start.isGreater(from)
                    : occurrence => occurrence.range.start.isSmaller(from)
            )
        } else {
            const next = direction === 'next'
            const start = from.line + (next ? 1 : -1)
            const increment = next ? 1 : -1

            for (let line = start; line >= 0 && line < this.lineIndex.length; line += increment) {
                occurrence = this.closestByCharacter(line, from)
                if (occurrence) {
                    break
                }
            }
        }
        return occurrence
    }

    // Returns the occurrence in the provided line number that is closest to the
    // provided position, compared by the character (not line). Returns undefined
    // when the line has no occurrences (for example, an empty string).
    private closestByCharacter(
        line: number,
        position: Position,
        includeOccurrence?: (occurrence: Occurrence) => boolean
    ): Occurrence | undefined {
        const candidates: [Occurrence, number][] = []
        let index = this.lineIndex[line] ?? -1
        for (; index >= 0 && index < this.length && this[index].range.start.line === line; index++) {
            const occurrence = this[index]
            if (includeOccurrence && !includeOccurrence(occurrence)) {
                continue
            }
            candidates.push([occurrence, occurrence.range.characterDistance(position)])
        }
        candidates.sort(([, a], [, b]) => a - b)
        if (candidates.length > 0) {
            return candidates[0][0]
        }
        return undefined
    }
}

// A facet that contains the precise code graph data from the occurrences API.
// It just retains the most recent contribution. At some point, we should
// probably extend this to be able to accept contributions from multiple
// sources.
export const codeGraphData = Facet.define<CodeGraphData[], IndexedCodeGraphData[]>({
    static: true,
    combine: values =>
        values[0]?.map(data => ({ ...data, occurrenceIndex: new OccurrenceIndex(data.occurrences) })) ?? [],
})
