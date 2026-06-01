package vision

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// ProcessResult contains the outcome of processing a payload through the registry.
type ProcessResult struct {
	Payload        []byte
	HasNewImages   bool
	ImagesFound    int
	HistoricalOnly bool
	RegistryNote   string
}

// Processor orchestrates the vision registry workflow for a single request.
type Processor struct {
	registry   *GlobalRegistry
	analyzer   ImageAnalyzer
	maxEntries int
}

// NewProcessor creates a Processor bound to the global registry.
func NewProcessor(analyzer ImageAnalyzer) *Processor {
	return &Processor{
		registry:   GetGlobal(),
		analyzer:   analyzer,
		maxEntries: DefaultGlobalConfig().MaxEntriesPerSession,
	}
}

func (p *Processor) Process(ctx context.Context, payload []byte, sessionKey SessionKey, turnIndex int) (*ProcessResult, error) {
	result := &ProcessResult{Payload: payload}

	walk := WalkPayload(payload)
	if len(walk.Parts) == 0 {
		return result, nil
	}
	result.ImagesFound = len(walk.Parts)

	hasSession := sessionKey != ""
	var sessionStore *SessionStore
	if hasSession {
		sessionStore = p.registry.GetOrCreateSession(sessionKey)
		sessionStore.ResetReachability()
	}
	ephemeralEntries := make(map[ImageHash]*ImageEntry)
	ephemeralNextOrdinal := 0

	// Separate current-turn from historical images
	var currentParts, historicalParts []ImagePart
	for _, part := range walk.Parts {
		if part.IsCurrent {
			currentParts = append(currentParts, part)
		} else {
			historicalParts = append(historicalParts, part)
		}
	}

	result.HasNewImages = len(currentParts) > 0
	result.HistoricalOnly = len(currentParts) == 0 && len(historicalParts) > 0

	// Record current-turn images in registry (don't replace them)
	for _, part := range currentParts {
		data, _ := ExtractImageData(&part)
		if data == "" {
			continue
		}
		if sessionStore == nil {
			continue
		}
		hash := ComputeHash(data)
		if sessionStore.GetEntry(hash) == nil {
			ordinal := sessionStore.NextOrdinal()
			sessionStore.GetOrCreateEntry(hash, ordinal, p.maxEntries)
		}
		sessionStore.UpdateEntry(hash, func(e *ImageEntry) {
			e.SourceKind = ImageSourceUserUpload
			if e.FirstSeenTurn == 0 {
				e.FirstSeenTurn = turnIndex
			}
			e.LastSeenTurn = turnIndex
			e.CurrentPayloadReachable = true
			e.Availability = ImageAvailableInline
			e.Occurrences = append(e.Occurrences, ImageOccurrence{
				TurnIndex:  turnIndex,
				MessageIdx: part.MsgIdx,
				PartIdx:    part.PartIdx,
			})
		})
	}

	// Determine the array type for content type decisions
	arrayType := detectArrayType(payload) // "messages" → "text", "input" → "input_text"

	// Build a map of hash → image data for re-analysis (extract BEFORE replacement)
	imageDataByHash := make(map[ImageHash]ImageDataInfo)
	for _, part := range historicalParts {
		data, mime := ExtractImageData(&part)
		if data == "" {
			continue
		}
		hash := ComputeHash(data)
		imageDataByHash[hash] = ImageDataInfo{Data: data, MIMEType: mime}
	}

	// Replace historical images with placeholders
	for _, part := range historicalParts {
		data, _ := ExtractImageData(&part)
		if data == "" {
			continue
		}
		hash := ComputeHash(data)
		entry := p.findOrCreateEntry(sessionStore, hash, turnIndex, ephemeralEntries, &ephemeralNextOrdinal)
		if sessionStore != nil {
			sessionStore.UpdateEntry(hash, func(e *ImageEntry) {
				e.SourceKind = ImageSourceUserUpload
				if e.FirstSeenTurn == 0 {
					e.FirstSeenTurn = turnIndex
				}
				e.LastSeenTurn = turnIndex
				e.CurrentPayloadReachable = true
				e.Availability = ImageAvailableInline
				e.Occurrences = append(e.Occurrences, ImageOccurrence{
					TurnIndex:  turnIndex,
					MessageIdx: part.MsgIdx,
					PartIdx:    part.PartIdx,
				})
			})
		}
		placeholder := BuildShortPlaceholder(entry)

		var err error
		result.Payload, err = ReplaceImagePartEx(result.Payload, part, placeholder, arrayType)
		if err != nil {
			log.Warnf("vision: replace image part: %v", err)
		}
	}

	// Detect intent and generate registry note
	if sessionStore != nil && len(historicalParts) > 0 {
		lastMsg := extractLastUserText(result.Payload)
		entries := sessionStore.AllEntries()
		refNum := ExtractImageReference(lastMsg)

		if refNum > 0 {
			p.handleReAnalysis(ctx, sessionStore, refNum, lastMsg, imageDataByHash, result)
		} else {
			intent := DetectIntent(lastMsg, len(entries))
			switch intent {
			case IntentFollowUp:
				result.RegistryNote = BuildRegistryNote(entries, 0)
			case IntentAmbiguous:
				result.RegistryNote = BuildAmbiguityNote(entries)
			}
		}

		if result.RegistryNote != "" {
			var err error
			result.Payload, err = InjectRegistryNoteEx(result.Payload, result.RegistryNote, arrayType)
			if err != nil {
				log.Warnf("vision: inject registry note: %v", err)
			}
		}
	}

	return result, nil
}

// ImageDataInfo holds the raw image data and MIME type for re-analysis.
type ImageDataInfo struct {
	Data     string
	MIMEType string
}

func (p *Processor) findOrCreateEntry(sessionStore *SessionStore, hash ImageHash, turnIndex int, ephemeralEntries map[ImageHash]*ImageEntry, ephemeralNextOrdinal *int) *ImageEntry {
	if sessionStore == nil {
		if entry, ok := ephemeralEntries[hash]; ok {
			return entry
		}
		*ephemeralNextOrdinal = *ephemeralNextOrdinal + 1
		entry := &ImageEntry{
			Hash:                    hash,
			StableOrdinal:           *ephemeralNextOrdinal,
			CurrentPayloadReachable: true,
			Availability:            ImageAvailableInline,
			Summary:                 ImageSummary{Confidence: "low"},
		}
		ephemeralEntries[hash] = entry
		return entry
	}
	existing := sessionStore.GetEntry(hash)
	if existing != nil {
		sessionStore.UpdateEntry(hash, func(e *ImageEntry) {
			e.LastSeenTurn = turnIndex
			e.LastAccessAt = time.Now()
		})
		return existing
	}

	ordinal := sessionStore.NextOrdinal()
	entry := sessionStore.GetOrCreateEntry(hash, ordinal, p.maxEntries)
	return entry
}

func (p *Processor) handleReAnalysis(ctx context.Context, sessionStore *SessionStore, targetOrdinal int, query string, imageDataByHash map[ImageHash]ImageDataInfo, result *ProcessResult) {
	entries := sessionStore.AllEntries()
	var target *ImageEntry
	for _, e := range entries {
		if e.StableOrdinal == targetOrdinal {
			target = e
			break
		}
	}
	if target == nil {
		return
	}

	imgInfo, hasData := imageDataByHash[target.Hash]
	if !hasData || imgInfo.Data == "" {
		result.RegistryNote = BuildRegistryNote([]*ImageEntry{target}, targetOrdinal)
		return
	}

	req := AnalyzeRequest{
		Existing:   target.Summary,
		Query:      query,
		IsFollowUp: target.Summary.Summary != "",
		SourceKind: target.SourceKind,
		ImageData:  imgInfo.Data,
		MIMEType:   imgInfo.MIMEType,
	}

	resp, err := p.analyzer.Analyze(ctx, req)
	if err != nil {
		log.Warnf("vision: re-analysis failed for Image #%d: %v", targetOrdinal, err)
		result.RegistryNote = BuildRegistryNote([]*ImageEntry{target}, targetOrdinal)
		return
	}

	// Merge all summary fields, not just Summary
	sessionStore.UpdateEntry(target.Hash, func(e *ImageEntry) {
		merged := resp.Summary
		merged.Summary = mergeSummaries(e.Summary.Summary, resp.Summary.Summary)
		merged.OCRHints = mergeHints(e.Summary.OCRHints, resp.Summary.OCRHints, 5)
		merged.LayoutHints = mergeHints(e.Summary.LayoutHints, resp.Summary.LayoutHints, 5)
		merged.DetailHints = mergeHints(e.Summary.DetailHints, resp.Summary.DetailHints, 8)
		merged.Confidence = "high"
		e.Summary = merged
		e.LastAnalyzedAt = time.Now()
	})

	updated := sessionStore.GetEntry(target.Hash)
	if updated != nil {
		result.RegistryNote = BuildRegistryNote([]*ImageEntry{updated}, targetOrdinal)
	}
}

// extractLastUserText finds the text content of the last user message from the payload.
func extractLastUserText(payload []byte) string {
	items := gjson.GetBytes(payload, "messages")
	if !items.Exists() || !items.IsArray() {
		items = gjson.GetBytes(payload, "input")
	}
	if !items.Exists() || !items.IsArray() {
		return ""
	}

	// Find last user message
	lastUserIdx := -1
	arr := items.Array()
	for i := len(arr) - 1; i >= 0; i-- {
		if arr[i].Get("role").String() == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		return ""
	}

	content := arr[lastUserIdx].Get("content")
	if !content.Exists() {
		return ""
	}

	if content.Type == gjson.String {
		return content.String()
	}

	if content.IsArray() {
		var parts []string
		for _, part := range content.Array() {
			text := part.Get("text").String()
			if text == "" {
				text = part.Get("input_text").String()
			}
			if text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return parts[len(parts)-1] // last text part (most recent)
		}
	}

	return ""
}

// detectArrayType determines whether the payload uses "messages" or "input".
func detectArrayType(payload []byte) string {
	if gjson.GetBytes(payload, "messages").Exists() {
		return "messages"
	}
	if gjson.GetBytes(payload, "input").Exists() {
		return "input"
	}
	return "messages"
}

// CurrentTurnHasImages is a helper for executors.
func CurrentTurnHasImages(payload []byte) bool {
	walk := WalkPayload(payload)
	for _, p := range walk.Parts {
		if p.IsCurrent {
			return true
		}
	}
	return false
}

func mergeSummaries(existing, newSummary string) string {
	if existing == "" {
		return newSummary
	}
	return existing + " | " + newSummary
}

func mergeHints(existing, newHints []string, max int) []string {
	seen := make(map[string]bool, len(existing)+len(newHints))
	merged := make([]string, 0, max)
	for _, h := range existing {
		if len(merged) >= max {
			break
		}
		seen[h] = true
		merged = append(merged, h)
	}
	for _, h := range newHints {
		if len(merged) >= max {
			break
		}
		if !seen[h] {
			seen[h] = true
			merged = append(merged, h)
		}
	}
	return merged
}
