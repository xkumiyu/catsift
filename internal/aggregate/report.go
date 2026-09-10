package aggregate

import (
	"sort"
	"time"

	"github.com/xkumiyu/catsift/internal/skillinventory"
	"github.com/xkumiyu/catsift/internal/usage"
)

// Input is the normalized stream consumed by all report views.
type Input struct {
	Turns        []usage.Turn
	SessionCount int
	Warnings     []usage.Warning
	Source       usage.SourceKind
	Agents       []string
}

// SkillGroupBy controls the unit counted for Skill usage.
type SkillGroupBy string

const (
	SkillGroupByTurn    SkillGroupBy = "turn"
	SkillGroupBySession SkillGroupBy = "session"
)

func (groupBy SkillGroupBy) Valid() bool {
	return groupBy == SkillGroupByTurn || groupBy == SkillGroupBySession
}

type Overview struct {
	Sessions            int
	Turns               int
	UserPrompts         int
	ToolCalls           int
	SkillUsesTurn       int
	SkillUsesSession    int
	TokenUsage          usage.TokenUsage
	TokenUsageAvailable bool
}

type ToolRow struct {
	Name     string
	Calls    int
	Failures int
	LastUsed time.Time
}

type SkillRow struct {
	Name        string
	Explicit    int
	Implicit    int
	Unknown     int
	Confirmed   int
	Inferred    int
	Unconfirmed int
	Total       int
	LastUsed    time.Time
}

type Report struct {
	Overview        Overview
	Tools           []ToolRow
	Skills          []SkillRow
	UnusedSkills    []skillinventory.InventoryEntry
	InstalledSkills int
	UnusedRoots     []string
	Warnings        []usage.Warning
}

// BuildOverview computes all aggregate views from normalized turns. The
// effective view is used for overview Tool Calls, while model/runtime rows are
// retained for the tools command. Skill uses are counted both per turn and per
// session so the stats command can report both units together.
func BuildOverview(input Input) Report {
	result := Report{Warnings: append([]usage.Warning(nil), input.Warnings...)}
	sessions := make(map[string]struct{})
	allSkills := make([]usage.SkillEvidence, 0)
	for _, turn := range input.Turns {
		result.Overview.Turns++
		if turn.SessionID != "" {
			sessions[turn.SessionID] = struct{}{}
		}
		result.Overview.UserPrompts += turn.UserPrompts
		if turn.TokenUsage != nil {
			result.Overview.TokenUsageAvailable = true
			result.Overview.TokenUsage.Add(*turn.TokenUsage)
		}
		effective := usage.EffectiveTools(turn)
		result.Overview.ToolCalls += len(effective)
		allSkills = append(allSkills, turn.SkillEvidence...)
	}
	result.Overview.Sessions = input.SessionCount
	if result.Overview.Sessions == 0 {
		result.Overview.Sessions = len(sessions)
	}
	uses := usage.MergeSkillEvidence(allSkills)
	result.Overview.SkillUsesTurn = len(uses)
	result.Overview.SkillUsesSession = len(groupSkillUses(uses, SkillGroupBySession))
	result.Tools = aggregateTools(input.Turns, usage.LayerEffective)
	result.Skills = aggregateSkills(uses, false)
	return result
}

func Tools(input Input, layer usage.ToolLayer) []ToolRow {
	return aggregateTools(input.Turns, layer)
}

func Skills(input Input, strict bool) []SkillRow {
	return SkillsBy(input, strict, SkillGroupByTurn)
}

// SkillsBy returns Skill rows counted by turn or session. The existing
// Skills function remains the turn-grouped compatibility default.
func SkillsBy(input Input, strict bool, groupBy SkillGroupBy) []SkillRow {
	evidence := make([]usage.SkillEvidence, 0)
	for _, turn := range input.Turns {
		evidence = append(evidence, turn.SkillEvidence...)
	}
	return aggregateSkills(groupSkillUses(usage.MergeSkillEvidence(evidence), groupBy), strict)
}

// UnusedSkills returns physical inventory entries whose canonical names do not
// appear in the selected Skill usage view.
func UnusedSkills(input Input, inventory []skillinventory.InventoryEntry, strict bool, groupBy SkillGroupBy) []skillinventory.InventoryEntry {
	used := make(map[string]struct{})
	for _, row := range SkillsBy(input, strict, groupBy) {
		used[row.Name] = struct{}{}
	}
	result := make([]skillinventory.InventoryEntry, 0, len(inventory))
	for _, entry := range inventory {
		if _, ok := used[entry.Name]; ok {
			continue
		}
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].Path < result[j].Path
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// BuildUnusedReport combines a filesystem snapshot with the selected history
// view while keeping inventory rows separate from usage rows.
func BuildUnusedReport(input Input, snapshot skillinventory.InventorySnapshot, strict bool, groupBy SkillGroupBy) Report {
	result := Report{
		UnusedSkills:    UnusedSkills(input, snapshot.Entries, strict, groupBy),
		InstalledSkills: snapshot.InstalledCount,
		UnusedRoots:     append([]string(nil), snapshot.Roots...),
		Warnings:        append([]usage.Warning(nil), input.Warnings...),
	}
	result.Warnings = append(result.Warnings, snapshot.Warnings...)
	return result
}

func groupSkillUses(uses []usage.SkillUse, groupBy SkillGroupBy) []usage.SkillUse {
	if groupBy != SkillGroupBySession {
		return uses
	}
	type entry struct {
		use   usage.SkillUse
		modes map[usage.SkillMode]struct{}
	}
	entries := make(map[string]*entry, len(uses))
	for _, item := range uses {
		key := string(item.Source.Source) + "\x00" + item.Source.Agent + "\x00" + item.SessionID + "\x00" + item.SkillName
		current, ok := entries[key]
		if !ok {
			copy := item
			copy.Modes = nil
			current = &entry{use: copy, modes: make(map[usage.SkillMode]struct{})}
			entries[key] = current
		}
		for _, mode := range skillUseModes(item) {
			current.modes[mode] = struct{}{}
		}
		if skillStateRank(item.State) > skillStateRank(current.use.State) {
			current.use.State = item.State
		}
		if current.use.Timestamp.IsZero() || (!item.Timestamp.IsZero() && item.Timestamp.After(current.use.Timestamp)) {
			current.use.Timestamp = item.Timestamp
			current.use.Source = item.Source
		}
	}
	result := make([]usage.SkillUse, 0, len(entries))
	for _, current := range entries {
		current.use.Modes = make([]usage.SkillMode, 0, len(current.modes))
		for mode := range current.modes {
			current.use.Modes = append(current.use.Modes, mode)
		}
		sort.Slice(current.use.Modes, func(i, j int) bool { return current.use.Modes[i] < current.use.Modes[j] })
		if len(current.use.Modes) > 0 {
			current.use.Mode = current.use.Modes[0]
		}
		result = append(result, current.use)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Timestamp.Equal(result[j].Timestamp) {
			if result[i].SessionID == result[j].SessionID {
				return result[i].SkillName < result[j].SkillName
			}
			return result[i].SessionID < result[j].SessionID
		}
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result
}

func skillUseModes(use usage.SkillUse) []usage.SkillMode {
	if len(use.Modes) > 0 {
		return use.Modes
	}
	if use.Mode == "" {
		return nil
	}
	return []usage.SkillMode{use.Mode}
}

func skillStateRank(state usage.SkillState) int {
	switch state {
	case usage.StateConfirmed:
		return 3
	case usage.StateInferred:
		return 2
	case usage.StateUnconfirmed:
		return 1
	default:
		return 0
	}
}

func aggregateTools(turns []usage.Turn, layer usage.ToolLayer) []ToolRow {
	rows := make(map[string]*ToolRow)
	for _, turn := range turns {
		observations := usage.ToolsForLayer(turn, layer)
		for _, obs := range observations {
			name := obs.CanonicalName
			if name == "" {
				name = obs.RawName
			}
			row := rows[name]
			if row == nil {
				row = &ToolRow{Name: name}
				rows[name] = row
			}
			row.Calls++
			if obs.Status == usage.StatusFailure {
				row.Failures++
			}
			if row.LastUsed.IsZero() || obs.Timestamp.After(row.LastUsed) {
				row.LastUsed = obs.Timestamp
			}
		}
	}
	result := make([]ToolRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Calls == result[j].Calls {
			return result[i].Name < result[j].Name
		}
		return result[i].Calls > result[j].Calls
	})
	return result
}

func aggregateSkills(uses []usage.SkillUse, strict bool) []SkillRow {
	rows := make(map[string]*SkillRow)
	for _, use := range uses {
		if strict && use.State != usage.StateConfirmed {
			continue
		}
		row := rows[use.SkillName]
		if row == nil {
			row = &SkillRow{Name: use.SkillName}
			rows[use.SkillName] = row
		}
		row.Total++
		if use.HasMode(usage.ModeExplicit) {
			row.Explicit++
		}
		if use.HasMode(usage.ModeImplicit) {
			row.Implicit++
		}
		if use.HasMode(usage.ModeUnknown) {
			row.Unknown++
		}
		switch use.State {
		case usage.StateConfirmed:
			row.Confirmed++
		case usage.StateInferred:
			row.Inferred++
		case usage.StateUnconfirmed:
			row.Unconfirmed++
		}
		if row.LastUsed.IsZero() || use.Timestamp.After(row.LastUsed) {
			row.LastUsed = use.Timestamp
		}
	}
	result := make([]SkillRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Total == result[j].Total {
			return result[i].Name < result[j].Name
		}
		return result[i].Total > result[j].Total
	})
	return result
}
