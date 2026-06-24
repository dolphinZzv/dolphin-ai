package dream

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"dolphin/internal/llm"
	"dolphin/internal/types"
)

// edit implements Phase 2: send proposals to LLM and receive refined edits.
func (d *Dream) edit(ctx context.Context, proposals []EditProposal) ([]Edit, error) {
	// Split: proposals needing LLM vs pure-rule (deprecation).
	var llmProposals []EditProposal
	var ruleEdits []Edit
	for _, p := range proposals {
		if !p.NeedsLLM {
			ruleEdits = append(ruleEdits, Edit{
				ProposalID: p.ID,
				Action:     p.Action,
				Target:     p.Target,
				After:      "", // deprecations need no content
				Reasoning:  p.Reason,
			})
		} else {
			llmProposals = append(llmProposals, p)
		}
	}

	// If no proposals need LLM, return rule-only edits.
	if len(llmProposals) == 0 {
		return ruleEdits, nil
	}

	// Build prompt and call LLM.
	prompt := d.buildEditPrompt(llmProposals)
	req := llm.LLMRequest{
		Messages: []types.Message{
			{Role: types.RoleUser, Content: prompt, Timestamp: time.Now()},
		},
		Model:     d.activeModel(),
		MaxTokens: d.maxReflectTokens,
		Stream:    true,
		Timeout:   60 * time.Second,
	}

	ch, err := d.provider.CompleteStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("llm call: %w", err)
	}

	var content strings.Builder
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				goto done
			}
			if chunk.Error != nil {
				return nil, fmt.Errorf("stream error: %w", chunk.Error)
			}
			content.WriteString(chunk.Content)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
done:

	// Parse JSON output.
	llmEdits, err := parseEditOutput(content.String())
	if err != nil {
		return nil, fmt.Errorf("parse llm output: %w", err)
	}

	// Combine rule-only and LLM edits.
	all := make([]Edit, 0, len(ruleEdits)+len(llmEdits))
	all = append(all, ruleEdits...)
	all = append(all, llmEdits...)
	return all, nil
}

// activeModel returns the model name for the Phase 2 LLM call.
func (d *Dream) activeModel() string {
	if d.reflectModel != "" {
		return d.reflectModel
	}
	return "" // provider will use its active model
}

// buildEditPrompt constructs the Phase 2 prompt with proposals and context.
func (d *Dream) buildEditPrompt(proposals []EditProposal) string {
	var b strings.Builder

	b.WriteString("You are editing brain files to make them more accurate and concise.\n\n")

	// Existing brain context.
	bc := d.buildBrainContext()
	b.WriteString("## Existing brain state\n")
	b.WriteString("Commands: " + strings.Join(bc.Commands, ", ") + "\n")
	b.WriteString("Facts: " + strings.Join(bc.Facts, ", ") + "\n\n")

	// Edit proposals.
	b.WriteString("## Edit proposals\n")
	for _, p := range proposals {
		b.WriteString(fmt.Sprintf("--- PROPOSAL %s ---\n", p.ID))
		b.WriteString(fmt.Sprintf("action: %s\n", p.Action))
		b.WriteString(fmt.Sprintf("target: %s\n", p.Target))
		b.WriteString(fmt.Sprintf("before: |\n  %s\n", strings.ReplaceAll(p.Before, "\n", "\n  ")))
		b.WriteString(fmt.Sprintf("reason: %s\n", p.Reason))
		b.WriteString("--- END " + p.ID + " ---\n\n")
	}

	// Few-shot examples.
	b.WriteString("## Examples\n")
	b.WriteString("Example 1 — improve:\n")
	b.WriteString("action: improve, target: commands/deploy.md\n")
	b.WriteString(`before: "1. Run: docker compose up -d"
reason: 用户纠正：用 kubectl 不是 docker compose
OUTPUT: {"proposal_id":"p1","action":"improve","target":"commands/deploy.md","after":"1. Apply: kubectl apply -f deploy.yaml\n2. Verify: kubectl rollout status deployment/app","reasoning":"将 docker compose→kubectl，curl→rollout status"}` + "\n\n")

	b.WriteString("Example 2 — create:\n")
	b.WriteString(`OUTPUT: {"proposal_id":"p2","action":"create","target":"knowledge/go-pref.md","after":"用户偏好 Go。后端/CLI 工具用 Go，不用 Python。","reasoning":"显式偏好声明"}` + "\n\n")

	b.WriteString("Example 3 — merge:\n")
	b.WriteString(`OUTPUT: {"proposal_id":"p3","action":"merge","target":"rules/deploy.md","after":"部署：k8s (kubectl apply) 是标准方案。Docker Compose 曾用但已废弃。","reasoning":"合并消除重复"}` + "\n\n")

	// Rules.
	b.WriteString("## Rules\n")
	b.WriteString("1. Output ONLY valid JSON array of edit objects. No markdown wrapping.\n")
	b.WriteString("2. Each edit: {\"proposal_id\":...,\"action\":...,\"target\":...,\"after\":...,\"reasoning\":...}\n")
	b.WriteString("3. improve/merge/create: after must be non-empty. deprecate: after must be null.\n")
	b.WriteString("4. Every improvement must have a reasoning.\n")
	b.WriteString(`5. After length ≤ before length for improve/merge (shorter is better).` + "\n")
	b.WriteString("6. Do not create new files if an existing brain file already covers the topic.\n")
	b.WriteString(`7. Output format: [{"proposal_id":"p1","action":"improve","target":"...","after":"...","reasoning":"..."}]` + "\n")

	return b.String()
}

// buildBrainContext produces a minimal summary of the current brain state.
func (d *Dream) buildBrainContext() BrainContext {
	files, _ := d.brain.List(context.Background())
	var cmds, facts []string
	for _, f := range files {
		switch {
		case strings.HasPrefix(f, "commands/"):
			cmds = append(cmds, strings.TrimSuffix(strings.TrimPrefix(f, "commands/"), ".md"))
		case strings.HasPrefix(f, "knowledge/"), strings.HasPrefix(f, "rules/"):
			facts = append(facts, strings.TrimSuffix(f, ".md"))
		}
	}
	return BrainContext{Commands: cmds, Facts: facts}
}

// parseEditOutput parses the JSON output from the Phase 2 LLM.
func parseEditOutput(raw string) ([]Edit, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var edits []Edit
	if err := json.Unmarshal([]byte(raw), &edits); err == nil {
		return edits, nil
	}
	var wrapper struct {
		Edits []Edit `json:"edits"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err == nil && len(wrapper.Edits) > 0 {
		return wrapper.Edits, nil
	}
	var single Edit
	if err := json.Unmarshal([]byte(raw), &single); err != nil {
		return nil, fmt.Errorf("parse edits: %w (raw: %.200s)", err, raw)
	}
	if single.ProposalID != "" {
		return []Edit{single}, nil
	}
	return nil, fmt.Errorf("parse edits: empty proposal_id")
}

// filterEdits removes edits that fail causality verification or quality checks.
func (d *Dream) filterEdits(edits []Edit, proposals []EditProposal) []Edit {
	// Build proposal lookup.
	propMap := make(map[string]*EditProposal, len(proposals))
	for i := range proposals {
		propMap[proposals[i].ID] = &proposals[i]
	}

	var passed []Edit
	for _, e := range edits {
		p, ok := propMap[e.ProposalID]
		if !ok {
			continue // proposal not found → reject.
		}

		// Quality checks.
		if e.Action == ActionDeprecate {
			// Always allow deprecations.
		} else if strings.TrimSpace(e.After) == "" {
			continue // empty after → reject.
		} else if len(e.Reasoning) == 0 {
			continue // no reasoning → reject.
		} else if p.Before != "" && jaccard(extractKeywords(p.Before), extractKeywords(e.After)) > 0.95 {
			continue // too similar → reject.
		}

		// Causality verification.
		if !d.verifyCausality(e, p, proposals) {
			continue
		}

		passed = append(passed, e)
	}
	return passed
}

// verifyCausality checks that an LLM edit has valid Phase 1 evidence.
func (d *Dream) verifyCausality(edit Edit, proposal *EditProposal, allProposals []EditProposal) bool {
	// 1. Rhetorical candidates must be paired with an L1/L2 proposal.
	if proposal.IsRhetorical {
		hasPair := false
		for _, other := range allProposals {
			if !other.IsRhetorical && other.Confidence >= 0.85 && shareEvidence(proposal, &other) {
				hasPair = true
				break
			}
		}
		if !hasPair {
			return false
		}
	}

	// 2. Create requires L1/L2 evidence.
	if edit.Action == ActionCreate {
		if proposal.Confidence < 0.85 || len(proposal.Evidence) == 0 {
			return false
		}
	}

	// 3. Merge requires confidence ≥ 0.7.
	if edit.Action == ActionMerge {
		if proposal.Confidence < 0.7 {
			return false
		}
	}

	// 4. Improve: Before must contain a pattern from the evidence.
	if edit.Action == ActionImprove && proposal.Before != "" {
		found := false
		for _, ev := range proposal.Evidence {
			if strings.Contains(proposal.Before, ev) {
				found = true
				break
			}
		}
		if !found {
			// Also check if the evidence keyword matches.
			for _, kw := range extractKeywords(proposal.Reason) {
				if strings.Contains(proposal.Before, kw) {
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func shareEvidence(a, b *EditProposal) bool {
	for _, ea := range a.Evidence {
		for _, eb := range b.Evidence {
			if ea == eb {
				return true
			}
		}
	}
	return false
}
