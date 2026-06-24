package dream

import (
	"context"
	"math"
	"path/filepath"
	"strings"
	"time"

	"dolphin/internal/types"
)

const (
	jaccardDedupThreshold = 0.7
	minRepeatSessions     = 3
	minRepeatPatterns     = 5
)

// scan implements Phase 1: extract edit signals from recent sessions and
// cross-reference with the current brain state.
func (d *Dream) scan(ctx context.Context) ([]EditProposal, error) {
	// 1. Get recent sessions.
	sessions, err := d.sessionMgr.List(ctx)
	if err != nil {
		return nil, err
	}
	var newSessions []sessionSnapshot
	for _, s := range sessions {
		if s.CreatedAt.After(d.state.LastDreamAt) {
			msgs, _ := d.memory.Read(ctx, s.ID)
			newSessions = append(newSessions, sessionSnapshot{ID: s.ID, Messages: msgs})
		}
	}

	// 2. Extract signals from sessions.
	extract := d.extractFromSessions(newSessions)

	// 3. Scan brain files.
	brainFiles, err := d.scanBrainFiles(ctx)
	if err != nil {
		return nil, err
	}

	// 4. Cross-reference signals with brain files → proposals.
	return d.buildProposals(extract, brainFiles), nil
}

// extractFromSessions analyses session snapshots for improvement signals.
func (d *Dream) extractFromSessions(sessions []sessionSnapshot) *ExtractResult {
	var res ExtractResult
	res.Sessions = len(sessions)

	for _, snap := range sessions {
		res.TotalMessages += len(snap.Messages)

		var roundAssistMsgs []types.Message

		for _, m := range snap.Messages {
			// Skip compaction summaries.
			if m.IsSummary {
				continue
			}

			res.TotalMessages++

			switch m.Role { //nolint:exhaustive // RoleSystem not in memory
			case types.RoleUser:
				content := strings.TrimSpace(m.Content)
				if content == "" {
					continue
				}
				res.TotalRounds++

				// Detect explicit fact/preference declarations.
				if detectExplicitPreference(content) {
					res.ExplicitFacts = append(res.ExplicitFacts, content)
				}
				if detectExplicitCommand(content) {
					res.ExplicitCommands = append(res.ExplicitCommands, content)
				}

				// Check for corrections in the previous round.
				if detectCorrection(content) && len(roundAssistMsgs) > 0 {
					tm := TeachableMoment{
						SessionID:      snap.ID,
						UserCorrection: truncateStr(content, 200),
						IsPreference:   detectIsPreference(content),
					}
					// Capture the last assistant action before correction.
					lastAsst := roundAssistMsgs[len(roundAssistMsgs)-1]
					tm.WrongAction = truncateStr(lastAsst.Content, 200)
					// Check if assistant changed behaviour after the correction.
					if d.didAssistantRespondAfterCorrection(snap.Messages, m) {
						res.TeachableMoments = append(res.TeachableMoments, tm)
					}
				}
				roundAssistMsgs = nil

			case types.RoleAssistant:
				roundAssistMsgs = append(roundAssistMsgs, m)

			case types.RoleTool:
				if m.IsError {
					res.ToolErrors = append(res.ToolErrors, ToolError{
						ToolName:  m.ToolCallID,
						ErrorMsg:  truncateStr(m.Content, 200),
						SessionID: snap.ID,
						Count:     1,
					})
				}
			}
		}

		// Detect repeated command patterns across sessions.
		d.extractRepeatedPatterns(snap, &res)
	}

	// Cross-session dedup: similar questions.
	d.extractSimilarQuestions(sessions, &res)

	return &res
}

// didAssistantRespondAfterCorrection checks if the assistant's behaviour
// changed after the user's correction message.
func (d *Dream) didAssistantRespondAfterCorrection(msgs []types.Message, correctionMsg types.Message) bool {
	for i, m := range msgs {
		if m.Timestamp.Equal(correctionMsg.Timestamp) || m.Timestamp.After(correctionMsg.Timestamp) {
			// Look for the next assistant message after the correction.
			for j := i + 1; j < len(msgs); j++ {
				if msgs[j].Role == types.RoleAssistant {
					return true
				}
			}
			return false
		}
	}
	return false
}

// extractRepeatedPatterns finds tool calls that recur across sessions.
func (d *Dream) extractRepeatedPatterns(snap sessionSnapshot, res *ExtractResult) {
	patterns := make(map[string]int)
	for _, m := range snap.Messages {
		if m.Role == types.RoleTool && m.ToolCallID != "" {
			// Extract tool name from the content or use the call ID prefix.
			parts := strings.SplitN(m.ToolCallID, "_", 3)
			if len(parts) >= 2 {
				pattern := parts[1]
				patterns[pattern]++
			}
		}
	}
	for pattern, count := range patterns {
		if count >= minRepeatPatterns {
			res.RepeatedCommands = append(res.RepeatedCommands, RepeatedPattern{
				ToolName: pattern,
				Pattern:  pattern,
				Count:    count,
				Sessions: []string{snap.ID},
			})
		}
	}
}

// extractSimilarQuestions finds semantically similar user questions across sessions.
func (d *Dream) extractSimilarQuestions(sessions []sessionSnapshot, res *ExtractResult) {
	type qEntry struct {
		text    string
		session string
		ts      time.Time
	}
	var questions []qEntry
	for _, snap := range sessions {
		for _, m := range snap.Messages {
			if m.Role == types.RoleUser && len(m.Content) > 20 && !strings.HasPrefix(m.Content, "/") {
				questions = append(questions, qEntry{
					text:    strings.TrimSpace(m.Content),
					session: snap.ID,
					ts:      m.Timestamp,
				})
			}
		}
	}

	// Simple keyword-overlap grouping (O(n²) but n is small in practice).
	grouped := make([]bool, len(questions))
	for i := 0; i < len(questions); i++ {
		if grouped[i] {
			continue
		}

		var group SimilarGroup
		keywords := extractKeywords(questions[i].text)
		group.Keywords = keywords
		group.Questions = append(group.Questions, truncateStr(questions[i].text, 100))
		group.Sessions = append(group.Sessions, questions[i].session)
		group.FirstAsked = questions[i].ts
		group.LastAsked = questions[i].ts
		grouped[i] = true

		for j := i + 1; j < len(questions); j++ {
			if grouped[j] {
				continue
			}
			if jaccard(keywords, extractKeywords(questions[j].text)) > jaccardDedupThreshold {
				group.Questions = append(group.Questions, truncateStr(questions[j].text, 100))
				group.Sessions = append(group.Sessions, questions[j].session)
				if questions[j].ts.Before(group.FirstAsked) {
					group.FirstAsked = questions[j].ts
				}
				if questions[j].ts.After(group.LastAsked) {
					group.LastAsked = questions[j].ts
				}
				grouped[j] = true
			}
		}

		group.RepeatedDays = int(group.LastAsked.Sub(group.FirstAsked).Hours() / 24)
		if len(group.Sessions) >= 2 {
			res.SimilarQuestions = append(res.SimilarQuestions, group)
		}
	}
}

// scanBrainFiles reads brain state for cross-referencing.
func (d *Dream) scanBrainFiles(ctx context.Context) ([]BrainFile, error) {
	files, err := d.brain.List(ctx)
	if err != nil {
		return nil, err
	}

	var out []BrainFile
	for _, f := range files {
		bf := BrainFile{Path: f}
		if usage, ok := d.state.Usage.Files[f]; ok {
			bf.ReferencedCount = usage.Refs
			bf.LastReferenced = usage.Last
		}
		// Read file size.
		if content, err := d.brain.Read(ctx, f); err == nil {
			bf.Size = len(content)
		}
		out = append(out, bf)
	}
	return out, nil
}

// buildProposals cross-references extracted signals with brain files to
// produce weighted, deduplicated edit proposals.
func (d *Dream) buildProposals(extract *ExtractResult, brainFiles []BrainFile) []EditProposal {
	var proposals []EditProposal
	idSeq := 0

	// Build a lookup of brain files by path for impact computation.
	brainMap := make(map[string]BrainFile, len(brainFiles))
	for _, bf := range brainFiles {
		brainMap[bf.Path] = bf
	}

	// Create proposals from teachable moments.
	for _, tm := range extract.TeachableMoments {
		target := d.inferTarget(tm.WrongAction, brainFiles)
		sig := EditSignal{
			Type:        SignalCorrection,
			Description: tm.UserCorrection,
			Evidence:    []string{tm.SessionID},
			Confidence:  0.85,
		}
		if tm.IsPreference {
			sig.Type = SignalPreference
			sig.Confidence = 0.95
		}
		if len(extract.TeachableMoments) >= 2 {
			sig.Confidence += 0.05
		}

		bf := brainMap[target]
		impact := d.computeImpact(sig, bf)
		if impact < d.minImpactThreshold && impact > 0 {
			continue
		}

		before := ""
		if content, err := d.brain.Read(context.Background(), target); err == nil {
			before = content
		}

		proposals = append(proposals, EditProposal{
			ID:         formatID(idSeq),
			Target:     target,
			Action:     ActionImprove,
			Before:     before,
			Reason:     sig.Description,
			Evidence:   sig.Evidence,
			Confidence: sig.Confidence,
			Impact:     impact,
			NeedsLLM:   impact >= d.minImpactThreshold,
		})
		idSeq++
	}

	// Repeated patterns → create_command or improve existing.
	for _, rp := range extract.RepeatedCommands {
		sig := EditSignal{
			Type:        SignalRepetition,
			Description: rp.Pattern + " used " + formatInt(rp.Count) + " times",
			Evidence:    rp.Sessions,
			Confidence:  0.80,
			FirstSeen:   time.Now(),
		}

		// Check if a command already covers this.
		existing := d.findMatchingCommand(rp.Pattern, brainFiles)
		if existing != "" {
			bf := brainMap[existing]
			impact := d.computeImpact(sig, bf)
			if impact >= d.minImpactThreshold {
				before := ""
				if content, err := d.brain.Read(context.Background(), existing); err == nil {
					before = content
				}
				proposals = append(proposals, EditProposal{
					ID:         formatID(idSeq),
					Target:     existing,
					Action:     ActionImprove,
					Before:     before,
					Reason:     sig.Description,
					Evidence:   sig.Evidence,
					Confidence: sig.Confidence,
					Impact:     impact,
					NeedsLLM:   impact >= d.minImpactThreshold,
				})
				idSeq++
			}
		} else if len(rp.Sessions) >= minRepeatSessions {
			// New command.
			impact := d.computeImpact(sig, BrainFile{})
			if impact >= d.minImpactThreshold {
				target := filepath.Join("commands", sanitiseFilename(rp.Pattern)+".md")
				proposals = append(proposals, EditProposal{
					ID:         formatID(idSeq),
					Target:     target,
					Action:     ActionCreate,
					Before:     "",
					Reason:     sig.Description,
					Evidence:   sig.Evidence,
					Confidence: sig.Confidence,
					Impact:     impact,
					NeedsLLM:   true,
				})
				idSeq++
			}
		}
	}

	// Similar questions → create_fact or merge.
	for _, sq := range extract.SimilarQuestions {
		if sq.RepeatedDays < 3 {
			continue
		}
		sig := EditSignal{
			Type:        SignalRepetition,
			Description: "repeated question across " + formatInt(len(sq.Sessions)) + " sessions",
			Evidence:    sq.Sessions,
			Confidence:  0.75,
			FirstSeen:   sq.FirstAsked,
		}
		// Try to find a matching fact.
		matching := d.findMatchingFact(sq.Keywords, brainFiles)
		impact := d.computeImpact(sig, BrainFile{})

		if matching != "" {
			bf := brainMap[matching]
			impact = d.computeImpact(sig, bf)
			before, _ := d.brain.Read(context.Background(), matching)
			proposals = append(proposals, EditProposal{
				ID:         formatID(idSeq),
				Target:     matching,
				Action:     ActionImprove,
				Before:     before,
				Reason:     sig.Description,
				Evidence:   sig.Evidence,
				Confidence: sig.Confidence,
				Impact:     impact,
				NeedsLLM:   impact >= d.minImpactThreshold,
			})
		} else if impact >= d.minImpactThreshold {
			keyword := strings.Join(sq.Keywords[:min(3, len(sq.Keywords))], "-")
			target := filepath.Join("knowledge", sanitiseFilename(keyword)+".md")
			proposals = append(proposals, EditProposal{
				ID:         formatID(idSeq),
				Target:     target,
				Action:     ActionCreate,
				Reason:     sig.Description,
				Evidence:   sig.Evidence,
				Confidence: sig.Confidence,
				Impact:     impact,
				NeedsLLM:   true,
			})
		}
		idSeq++
	}

	// Dedup fact files.
	proposals = append(proposals, d.detectFactMerges(brainFiles, &idSeq)...)

	// Detect unused commands (30+ days).
	for _, bf := range brainFiles {
		if bf.ReferencedCount > 0 {
			continue
		}
		if !strings.HasPrefix(bf.Path, "commands/") && !strings.HasPrefix(bf.Path, "scripts/") {
			continue
		}
		if bf.LastReferenced.IsZero() || time.Since(bf.LastReferenced) > 30*24*time.Hour {
			sig := EditSignal{
				Type:        SignalObsolescence,
				Description: bf.Path + " unused for 30+ days",
				FirstSeen:   bf.LastReferenced,
			}
			impact := d.computeImpact(sig, bf)
			proposals = append(proposals, EditProposal{
				ID:         formatID(idSeq),
				Target:     bf.Path,
				Action:     ActionDeprecate,
				Before:     "",
				Reason:     sig.Description,
				Confidence: 0.90,
				Impact:     impact,
				NeedsLLM:   false,
			})
			idSeq++
		}
	}

	// Sort by Impact descending. Dedup by Target (keep highest Impact).
	proposals = sortAndDedup(proposals)

	// Truncate to max.
	if len(proposals) > d.maxEditsPerDream {
		proposals = proposals[:d.maxEditsPerDream]
	}

	// Respect file cooldowns.
	var filtered []EditProposal
	for _, p := range proposals {
		if cd, ok := d.state.FileCooldowns[p.Target]; ok {
			if d.state.LastDreamID < cd.CooldownUntilDream {
				continue
			}
		}
		filtered = append(filtered, p)
	}

	return filtered
}

// computeImpact calculates the impact score for a signal and target.
func (d *Dream) computeImpact(signal EditSignal, target BrainFile) float64 {
	w := 1.0

	if target.ReferencedCount > 10 {
		w *= 2.0
	} else if target.ReferencedCount > 0 {
		w *= 1.2
	}
	// Not referenced → stays 1.0 (no penalty).

	switch signal.Type { //nolint:exhaustive // Preference and Repetition are handled via Confidence branch
	case SignalCorrection:
		w *= 1.5
	case SignalRefinement:
		w *= 0.8
	case SignalObsolescence:
		w *= 0.3
	}

	// Freshness: 7-day half-life.
	if !signal.FirstSeen.IsZero() {
		days := time.Since(signal.FirstSeen).Hours() / 24
		w *= max(0.1, math.Pow(0.5, days/7))
	}

	return w
}

// --- helpers ---

func detectExplicitPreference(content string) bool {
	return containsAny(content, "以后", "记住", "别忘了", "下次", "注意")
}

func detectExplicitCommand(content string) bool {
	return containsAny(content, "写一个", "创建", "加一个命令", "脚本", "帮我写")
}

func detectCorrection(content string) bool {
	return containsAny(content, "不对", "不是", "别", "应该", "以后")
}

func detectIsPreference(content string) bool {
	return containsAny(content, "以后都用", "以后别", "下次", "记住")
}

func (d *Dream) inferTarget(wrongAction string, brainFiles []BrainFile) string {
	// Map keywords from the wrong action to brain files.
	keywords := extractKeywords(wrongAction)
	for _, bf := range brainFiles {
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(bf.Path), strings.ToLower(kw)) {
				return bf.Path
			}
		}
	}
	// Default: commands/ if it looks like a command.
	if strings.Contains(wrongAction, "docker") || strings.Contains(wrongAction, "git") ||
		strings.Contains(wrongAction, "go ") || strings.Contains(wrongAction, "kubectl") {
		return "commands/" + sanitiseFilename(truncateStr(wrongAction, 30)) + ".md"
	}
	return "knowledge/refinement.md"
}

func (d *Dream) findMatchingCommand(pattern string, brainFiles []BrainFile) string {
	kw := extractKeywords(pattern)
	for _, bf := range brainFiles {
		if !strings.HasPrefix(bf.Path, "commands/") {
			continue
		}
		for _, k := range kw {
			if strings.Contains(strings.ToLower(bf.Path), strings.ToLower(k)) {
				return bf.Path
			}
		}
	}
	return ""
}

func (d *Dream) findMatchingFact(keywords []string, brainFiles []BrainFile) string {
	for _, bf := range brainFiles {
		if !strings.HasPrefix(bf.Path, "knowledge/") {
			continue
		}
		content, err := d.brain.Read(context.Background(), bf.Path)
		if err != nil {
			continue
		}
		kw2 := extractKeywords(content)
		if jaccard(keywords, kw2) > 0.5 {
			return bf.Path
		}
	}
	return ""
}

func (d *Dream) detectFactMerges(brainFiles []BrainFile, idSeq *int) []EditProposal {
	var proposals []EditProposal
	// Compare all fact pairs.
	for i := 0; i < len(brainFiles); i++ {
		for j := i + 1; j < len(brainFiles); j++ {
			if !strings.HasPrefix(brainFiles[i].Path, "knowledge/") || !strings.HasPrefix(brainFiles[j].Path, "knowledge/") {
				continue
			}
			ci, _ := d.brain.Read(context.Background(), brainFiles[i].Path)
			cj, _ := d.brain.Read(context.Background(), brainFiles[j].Path)
			if jaccard(extractKeywords(ci), extractKeywords(cj)) > jaccardDedupThreshold {
				*idSeq++
				proposals = append(proposals, EditProposal{
					ID:         formatID(*idSeq),
					Target:     brainFiles[i].Path,
					Action:     ActionMerge,
					Sources:    []string{brainFiles[j].Path},
					Before:     ci,
					Reason:     "content overlap with " + brainFiles[j].Path,
					Evidence:   []string{brainFiles[i].Path, brainFiles[j].Path},
					Confidence: 0.70,
					Impact:     0.5, // moderate — needs a threshold check anyway
					NeedsLLM:   true,
				})
				break
			}
		}
	}
	return proposals
}

// --- pure utility helpers (no LLM, no state) ---

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func formatID(n int) string { return "p" + formatInt(n+1) }

func formatInt(n int) string {
	s := ""
	for n > 0 {
		s = string(byte('0'+n%10)) + s
		n /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}

func extractKeywords(text string) []string {
	// Simplified keyword extraction: split on whitespace and common delimiters,
	// drop short words and stopwords.
	text = strings.ToLower(text)
	text = strings.NewReplacer(",", " ", ".", " ", ":", " ", ";", " ", "(", " ", ")", " ", "!", " ", "?", " ").Replace(text)
	words := strings.Fields(text)
	var kw []string
	stopwords := map[string]bool{
		"a": true, "an": true, "the": true, "is": true, "are": true, "was": true,
		"be": true, "been": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "and": true, "or": true, "it": true, "its": true,
		"this": true, "that": true, "with": true, "from": true, "by": true,
		"我": true, "你": true, "的": true, "了": true, "是": true, "在": true, "有": true,
		"可以": true, "一个": true, "一下": true,
	}
	seen := make(map[string]bool)
	for _, w := range words {
		if len(w) < 2 || stopwords[w] {
			continue
		}
		if !seen[w] {
			seen[w] = true
			kw = append(kw, w)
		}
	}
	return kw
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	set := make(map[string]bool)
	for _, x := range a {
		set[x] = true
	}
	intersection := 0
	for _, x := range b {
		if set[x] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func sanitiseFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		if r == ' ' {
			return '-'
		}
		return -1
	}, strings.ToLower(s))
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

func sortAndDedup(proposals []EditProposal) []EditProposal {
	// Sort by Impact descending.
	for i := 0; i < len(proposals); i++ {
		for j := i + 1; j < len(proposals); j++ {
			if proposals[j].Impact > proposals[i].Impact {
				proposals[i], proposals[j] = proposals[j], proposals[i]
			}
		}
	}
	// Dedup by Target.
	seen := make(map[string]bool)
	var deduped []EditProposal
	for _, p := range proposals {
		if seen[p.Target] {
			continue
		}
		seen[p.Target] = true
		deduped = append(deduped, p)
	}
	return deduped
}
