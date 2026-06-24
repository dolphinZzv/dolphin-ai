package dream

// tidy implements Phase 4: post-dream calibration and state updates.
func (d *Dream) tidy(edits []Edit) {
	// Update totals.
	for _, e := range edits {
		switch e.Action { //nolint:exhaustive // ActionSplit reserved
		case ActionImprove:
			d.state.Totals.FilesImproved++
		case ActionDeprecate:
			d.state.Totals.FilesDeprecated++
		case ActionMerge:
			d.state.Totals.FilesMerged++
		case ActionCreate:
			d.state.Totals.FilesCreated++
		}
	}

	// Update file cooldowns.
	for _, e := range edits {
		d.state.FileCooldowns[e.Target] = FileCooldown{
			LastEditedDream:    d.currentID,
			CooldownUntilDream: d.currentID + d.fileCooldownDreams,
		}
	}

	// Update last applied edits for implicit-feedback tracking (V2).
	d.state.LastAppliedEdits = nil
	for _, e := range edits {
		d.state.LastAppliedEdits = append(d.state.LastAppliedEdits, AppliedEdit{
			ID:     e.ProposalID,
			Target: e.Target,
		})
	}

	// Calibration: only for non-auto-apply mode (manual review provides signal).
	if !d.autoApply {
		d.calibrate(edits)
	}
}

// calibrate adjusts confidence thresholds based on manual review feedback.
func (d *Dream) calibrate(edits []Edit) {
	window := d.state.Calibration.Window
	// Keep only the most recent entries.
	if len(window) > d.calibrationWindow {
		window = window[len(window)-d.calibrationWindow:]
	}

	// For this run, all edits are "adopted" (passed through Phase 2 + 3).
	// In auto_apply: false mode, the user reviews these later via /dream accept/reject.
	// This calibration only runs when the user has explicitly accepted/rejected.
	// For auto_apply, this function is not called at all.

	thresholds := d.state.Calibration.Thresholds
	for sigType, threshold := range thresholds {
		adopted, total := countAdopted(window, sigType)
		if total == 0 {
			continue
		}
		rate := float64(adopted) / float64(total)

		if rate < 0.3 && threshold < d.calibrationCeiling {
			threshold += d.calibrationMinStep
		} else if rate > 0.7 && threshold > d.calibrationFloor {
			threshold -= d.calibrationMinStep
		}
		// 0.3-0.7: stable zone, don't adjust.
		thresholds[sigType] = threshold
	}
}

func countAdopted(window []CalibrationEntry, sigType SignalType) (int, int) {
	// Simplified: count across all window entries.
	adopted, total := 0, 0
	for _, e := range window {
		adopted += e.Adopted
		total += e.Total
	}
	return adopted, total
}
