package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/xkumiyu/catsift/internal/query"
	"github.com/xkumiyu/catsift/internal/usage"
)

// RenderQueryHuman renders the safe read-model views used by the models,
// skills, and sessions CLI commands. detailKey is empty for a list view.
func RenderQueryHuman(kind string, ctx ReportContext, model query.ReadModel, detailKey string, capabilities TerminalCapabilities) string {
	width := capabilities.Width
	if width <= 0 {
		width = 80
	}
	styled := capabilities.ColorsEnabled()
	detail := strings.TrimSpace(detailKey) != ""
	heading := queryHeading(kind, detail)
	if heading == "" {
		return ""
	}
	lines := []string{styleHeading(heading, styled)}
	lines = append(lines, queryContextLines(ctx, styled)...)
	lines = append(lines, "")
	switch kind {
	case "models":
		if detail {
			value, ok := model.ModelDetail(detailKey)
			if !ok {
				return ""
			}
			lines = append(lines, renderModelDetail(value, ctx, width, styled)...)
		} else {
			lines = append(lines, renderModelList(model.Models, ctx, width, styled)...)
		}
	case "skills":
		value, ok := model.SkillDetail(detailKey)
		if !ok {
			return ""
		}
		lines = append(lines, renderSkillDetail(value, ctx, width, styled)...)
	case "sessions":
		if detail {
			value, ok := model.SessionDetail(detailKey)
			if !ok {
				return ""
			}
			lines = append(lines, renderSessionDetail(value, ctx, width, styled)...)
		} else {
			lines = append(lines, renderSessionList(model.Sessions, ctx, width, styled)...)
		}
	default:
		return ""
	}
	for index := range lines {
		lines[index] = truncate(lines[index], width)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func queryHeading(kind string, detail bool) string {
	switch kind {
	case "models":
		if detail {
			return "MODEL DETAIL"
		}
		return "MODEL USAGE"
	case "skills":
		return "SKILL DETAIL"
	case "sessions":
		if detail {
			return "SESSION DETAIL"
		}
		return "SESSION USAGE"
	default:
		return ""
	}
}

func queryContextLines(ctx ReportContext, styled bool) []string {
	return []string{
		styleContext("Source: "+ctx.sourceContext(), styled),
		styleContext("Agents: "+ctx.agent(), styled),
		styleContext("Period: "+ctx.period(), styled),
	}
}

func renderModelList(rows []query.ModelSummary, ctx ReportContext, width int, styled bool) []string {
	values := make([][]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, []string{
			modelDisplayName(row.Model),
			formatCount(row.Sessions),
			formatCount(row.Turns),
			formatQueryTokens(row.TokenUsage, row.TokenUsageAvailable),
			formatLocalTime(row.LastUsed, ctx.Location),
		})
	}
	return renderQueryTable([]string{"Model", "Sessions", "Turns", "Tokens", "Last Used"}, values, []bool{false, true, true, true, false}, width, styled, "No models in this scope.")
}

func renderModelDetail(detail query.ModelDetail, ctx ReportContext, width int, styled bool) []string {
	row := detail.Summary
	lines := querySummaryLines([]queryField{
		{label: "Model", value: modelDisplayName(row.Model)},
		{label: "Sessions", value: formatCount(row.Sessions)},
		{label: "Turns", value: formatCount(row.Turns)},
		{label: "Prompts", value: formatCount(row.UserPrompts)},
		{label: "Tools", value: formatCount(row.ToolCalls)},
		{label: "Skills", value: formatCount(row.SkillUses)},
		{label: "Tokens", value: formatQueryTokens(row.TokenUsage, row.TokenUsageAvailable)},
		{label: "First Used", value: formatLocalTime(row.FirstUsed, ctx.Location)},
		{label: "Last Used", value: formatLocalTime(row.LastUsed, ctx.Location)},
	}, styled)
	lines = append(lines, styleHeader("Sessions", styled))
	values := make([][]string, 0, len(detail.Sessions))
	for _, session := range detail.Sessions {
		values = append(values, []string{sessionDisplay(session.Title, session.ID), session.Project, formatCount(session.Turns), formatQueryTokens(session.TokenUsage, session.TokenUsage != (usage.TokenUsage{})), formatLocalTime(session.LastUsed, ctx.Location)})
	}
	lines = append(lines, renderQueryTable([]string{"Session", "Project", "Turns", "Tokens", "Last Used"}, values, []bool{false, false, true, true, false}, width, styled, "No related sessions.")...)
	return lines
}

func renderSkillDetail(detail query.SkillDetail, ctx ReportContext, width int, styled bool) []string {
	row := detail.Summary
	lines := querySummaryLines([]queryField{
		{label: "Skill", value: row.Name},
		{label: "Uses", value: formatCount(row.Uses)},
		{label: "Sessions", value: formatCount(row.Sessions)},
		{label: "Turns", value: formatCount(row.Turns)},
		{label: "Mode", value: formatSkillModes(row.ModeCounts)},
		{label: "State", value: formatSkillStates(row.StateCounts)},
		{label: "Method", value: formatSkillMethods(row.MethodCounts)},
		{label: "First Used", value: formatLocalTime(row.FirstUsed, ctx.Location)},
		{label: "Last Used", value: formatLocalTime(row.LastUsed, ctx.Location)},
	}, styled)
	lines = append(lines, styleHeader("Sessions", styled))
	values := make([][]string, 0, len(detail.Sessions))
	for _, session := range detail.Sessions {
		values = append(values, []string{sessionDisplay(session.Title, session.SessionID), session.Project, formatCount(session.Uses), formatCount(session.Turns), formatSkillModes(session.ModeCounts), formatSkillStates(session.StateCounts), formatLocalTime(session.LastUsed, ctx.Location)})
	}
	lines = append(lines, renderQueryTable([]string{"Session", "Project", "Uses", "Turns", "Mode", "State", "Last Used"}, values, []bool{false, false, true, true, false, false, false}, width, styled, "No related sessions.")...)
	return lines
}

func renderSessionList(rows []query.SessionSummary, ctx ReportContext, width int, styled bool) []string {
	values := make([][]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, []string{sessionDisplay(row.Title, row.ID), row.ID, displayProject(row.ProjectName, row.ProjectPath), formatLocalTime(row.EndedAt, ctx.Location)})
	}
	return renderQueryTable([]string{"Session", "ID", "Project", "Last Used"}, values, []bool{false, false, false, false}, width, styled, "No sessions in this scope.")
}

func renderSessionDetail(detail query.SessionDetail, ctx ReportContext, width int, styled bool) []string {
	row := detail.Summary
	lines := querySummaryLines([]queryField{
		{label: "Session", value: sessionDisplay(row.Title, row.ID)},
		{label: "Session ID", value: row.ID},
		{label: "Source", value: string(row.Source)},
		{label: "Agent", value: row.Agent},
		{label: "Provider", value: row.Provider},
		{label: "Project", value: displayProject(row.ProjectName, row.ProjectPath)},
		{label: "Started", value: formatLocalTime(row.StartedAt, ctx.Location)},
		{label: "Ended", value: formatLocalTime(row.EndedAt, ctx.Location)},
		{label: "Prompts", value: formatCount(row.UserPrompts)},
		{label: "Tools", value: formatCount(row.ToolCalls)},
		{label: "Skills", value: formatCount(row.SkillUses)},
	}, styled)
	lines = append(lines, "")
	lines = append(lines, renderStatSections([]statSection{{title: "Token Usage", metrics: tokenMetricsForUsage(row.TokenUsage, row.TokenUsageAvailable)}}, styled)...)
	lines = append(lines, "")
	lines = append(lines, styleHeader("Turns", styled))
	values := make([][]string, 0, len(detail.Turns))
	for _, turn := range detail.Turns {
		values = append(values, []string{turn.ID, modelNames(turn.Models), formatQueryTokens(turn.TokenUsage, turn.TokenUsageAvailable), strings.Join(turn.Tools, ", "), strings.Join(turn.Skills, ", "), queryTurnStatus(turn), formatLocalTime(turn.StartedAt, ctx.Location), formatLocalTime(turn.EndedAt, ctx.Location)})
	}
	lines = append(lines, renderQueryTable([]string{"Turn", "Model", "Tokens", "Tools", "Skills", "Status", "Started", "Ended"}, values, []bool{false, false, true, false, false, false, false, false}, width, styled, "No turns in this session.")...)
	return lines
}

type queryField struct {
	label string
	value string
}

func querySummaryLines(fields []queryField, styled bool) []string {
	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		value := field.value
		if strings.TrimSpace(value) == "" {
			value = "-"
		}
		lines = append(lines, "  "+padRight(styleLabel(field.label+":", styled), 13)+" "+safeDisplay(value))
	}
	return lines
}

func renderQueryTable(headers []string, rows [][]string, rightAligned []bool, width int, styled bool, empty string) []string {
	columns := queryTableColumns(headers, rows, rightAligned, width)
	lines := []string{renderQueryTableLine(headers, columns, styled, true), tableRule(queryTableWidth(columns), styled)}
	if len(rows) == 0 {
		return append(lines, styleNotice(empty, styled))
	}
	for _, row := range rows {
		lines = append(lines, renderQueryTableLine(row, columns, styled, false))
	}
	return lines
}

type queryTableColumn struct {
	width        int
	rightAligned bool
}

func queryTableColumns(headers []string, rows [][]string, rightAligned []bool, width int) []queryTableColumn {
	columns := make([]queryTableColumn, len(headers))
	for index, header := range headers {
		columnWidth := lipgloss.Width(safeDisplay(header))
		for _, row := range rows {
			if index < len(row) {
				columnWidth = maxInt(columnWidth, lipgloss.Width(safeDisplay(row[index])))
			}
		}
		columns[index] = queryTableColumn{width: maxInt(1, columnWidth)}
		if index < len(rightAligned) {
			columns[index].rightAligned = rightAligned[index]
		}
	}

	available := maxInt(len(columns), width-2*maxInt(0, len(columns)-1))
	for queryTableWidth(columns) > available {
		widest := -1
		for index, column := range columns {
			if column.width > 1 && (widest == -1 || column.width > columns[widest].width) {
				widest = index
			}
		}
		if widest == -1 {
			break
		}
		columns[widest].width--
	}
	return columns
}

func queryTableWidth(columns []queryTableColumn) int {
	width := 2 * maxInt(0, len(columns)-1)
	for _, column := range columns {
		width += column.width
	}
	return maxInt(1, width)
}

func renderQueryTableLine(values []string, columns []queryTableColumn, styled bool, header bool) string {
	cells := make([]string, len(columns))
	for index, column := range columns {
		value := ""
		if index < len(values) {
			value = safeDisplay(values[index])
		}
		value = truncate(value, column.width)
		if column.rightAligned {
			cells[index] = padLeft(value, column.width)
		} else {
			cells[index] = padRight(value, column.width)
		}
	}
	line := strings.Join(cells, "  ")
	if header {
		line = styleHeader(line, styled)
	}
	return line
}

func modelDisplayName(model usage.ModelRef) string {
	return model.Provider + "/" + model.Name
}

func sessionDisplay(title, id string) string {
	if strings.TrimSpace(title) == "" {
		return "(" + id + ")"
	}
	return title
}

func formatQueryTokens(value usage.TokenUsage, available bool) string {
	if !available {
		return "-"
	}
	return formatCompactCount64(value.TotalTokens)
}

func formatSkillModes(values map[usage.SkillMode]int) string {
	pairs := make([]string, 0, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	for _, key := range keys {
		pairs = append(pairs, key+"="+formatCount(values[usage.SkillMode(key)]))
	}
	return strings.Join(pairs, ", ")
}

func formatSkillStates(values map[usage.SkillState]int) string {
	pairs := make([]string, 0, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	for _, key := range keys {
		pairs = append(pairs, key+"="+formatCount(values[usage.SkillState(key)]))
	}
	return strings.Join(pairs, ", ")
}

func formatSkillMethods(values map[usage.SkillEvidenceMethod]int) string {
	pairs := make([]string, 0, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	for _, key := range keys {
		pairs = append(pairs, key+"="+formatCount(values[usage.SkillEvidenceMethod(key)]))
	}
	return strings.Join(pairs, ", ")
}

func modelNames(values []usage.ModelRef) string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, modelDisplayName(value))
	}
	return strings.Join(result, ", ")
}

func queryTurnStatus(value query.TurnSummary) string {
	if value.Aborted {
		return "aborted"
	}
	return "done"
}

type queryJSONMeta struct {
	SchemaVersion int                `json:"schema_version"`
	Agent         string             `json:"agent"`
	Source        string             `json:"source"`
	Sources       []usage.SourceKind `json:"sources,omitempty"`
	Agents        []string           `json:"agents"`
	Period        string             `json:"period"`
	GeneratedAt   string             `json:"generated_at"`
}

type modelSummaryJSON struct {
	Model               usage.ModelRef   `json:"model"`
	Sessions            int              `json:"sessions"`
	Turns               int              `json:"turns"`
	UserPrompts         int              `json:"user_prompts"`
	ToolCalls           int              `json:"tool_calls"`
	SkillUses           int              `json:"skill_uses"`
	TokenUsage          usage.TokenUsage `json:"token_usage"`
	TokenUsageAvailable bool             `json:"token_usage_available"`
	FirstUsed           string           `json:"first_used,omitempty"`
	LastUsed            string           `json:"last_used,omitempty"`
}

type modelSessionJSON struct {
	ID         string           `json:"id"`
	Title      string           `json:"title,omitempty"`
	Source     usage.SourceKind `json:"source,omitempty"`
	Agent      string           `json:"agent,omitempty"`
	Project    string           `json:"project,omitempty"`
	Turns      int              `json:"turns"`
	TokenUsage usage.TokenUsage `json:"token_usage"`
	FirstUsed  string           `json:"first_used,omitempty"`
	LastUsed   string           `json:"last_used,omitempty"`
}

type skillSummaryJSON struct {
	Name         string                            `json:"name"`
	Uses         int                               `json:"uses"`
	Sessions     int                               `json:"sessions"`
	Turns        int                               `json:"turns"`
	Explicit     int                               `json:"explicit"`
	Implicit     int                               `json:"implicit"`
	Unknown      int                               `json:"unknown"`
	Confirmed    int                               `json:"confirmed"`
	Inferred     int                               `json:"inferred"`
	Unconfirmed  int                               `json:"unconfirmed"`
	ModeCounts   map[usage.SkillMode]int           `json:"mode_counts,omitempty"`
	StateCounts  map[usage.SkillState]int          `json:"state_counts,omitempty"`
	MethodCounts map[usage.SkillEvidenceMethod]int `json:"method_counts,omitempty"`
	FirstUsed    string                            `json:"first_used,omitempty"`
	LastUsed     string                            `json:"last_used,omitempty"`
}

type skillSessionJSON struct {
	SessionID    string                            `json:"session_id"`
	Title        string                            `json:"title,omitempty"`
	Source       usage.SourceKind                  `json:"source,omitempty"`
	Agent        string                            `json:"agent,omitempty"`
	Project      string                            `json:"project,omitempty"`
	Uses         int                               `json:"uses"`
	Turns        int                               `json:"turns"`
	ModeCounts   map[usage.SkillMode]int           `json:"mode_counts,omitempty"`
	StateCounts  map[usage.SkillState]int          `json:"state_counts,omitempty"`
	MethodCounts map[usage.SkillEvidenceMethod]int `json:"method_counts,omitempty"`
	FirstUsed    string                            `json:"first_used,omitempty"`
	LastUsed     string                            `json:"last_used,omitempty"`
}

type sessionSummaryJSON struct {
	ID                  string           `json:"id"`
	Title               string           `json:"title,omitempty"`
	Source              usage.SourceKind `json:"source,omitempty"`
	Agent               string           `json:"agent,omitempty"`
	Provider            string           `json:"provider,omitempty"`
	ProviderSessionID   string           `json:"provider_session_id,omitempty"`
	CtxSessionID        string           `json:"ctx_session_id,omitempty"`
	ProjectName         string           `json:"project_name,omitempty"`
	ProjectPath         string           `json:"project_path,omitempty"`
	CLIVersion          string           `json:"cli_version,omitempty"`
	CreatedAt           string           `json:"created_at,omitempty"`
	UpdatedAt           string           `json:"updated_at,omitempty"`
	StartedAt           string           `json:"started_at,omitempty"`
	EndedAt             string           `json:"ended_at,omitempty"`
	Models              []usage.ModelRef `json:"models,omitempty"`
	Turns               int              `json:"turns"`
	UserPrompts         int              `json:"user_prompts"`
	ToolCalls           int              `json:"tool_calls"`
	SkillUses           int              `json:"skill_uses"`
	TokenUsage          usage.TokenUsage `json:"token_usage"`
	TokenUsageAvailable bool             `json:"token_usage_available"`
	Aborted             bool             `json:"aborted,omitempty"`
}

type turnSummaryJSON struct {
	ID                  string           `json:"id"`
	Ordinal             int              `json:"ordinal"`
	StartedAt           string           `json:"started_at,omitempty"`
	EndedAt             string           `json:"ended_at,omitempty"`
	Status              string           `json:"status"`
	Models              []usage.ModelRef `json:"models,omitempty"`
	TokenUsage          usage.TokenUsage `json:"token_usage"`
	TokenUsageAvailable bool             `json:"token_usage_available"`
	Tools               []string         `json:"tools,omitempty"`
	Skills              []string         `json:"skills,omitempty"`
}

func queryMeta(ctx ReportContext) queryJSONMeta {
	return queryJSONMeta{SchemaVersion: 1, Agent: ctx.agentID(), Source: string(ctx.sourceKind()), Sources: ctx.jsonSources(), Agents: ctx.agentIDs(), Period: ctx.period(), GeneratedAt: formatMachineTime(ctx.ReferenceTime)}
}

// RenderQueryJSON returns an explicit public schema for a Query read-model
// list or detail view. It intentionally does not marshal query.ReadModel.
func RenderQueryJSON(kind string, ctx ReportContext, model query.ReadModel, detailKey string) ([]byte, error) {
	detail := strings.TrimSpace(detailKey) != ""
	meta := queryMeta(ctx)
	switch kind {
	case "models":
		if detail {
			value, ok := model.ModelDetail(detailKey)
			if !ok {
				return nil, fmt.Errorf("model detail %q was not found", detailKey)
			}
			return json.MarshalIndent(struct {
				queryJSONMeta
				Summary  modelSummaryJSON   `json:"summary"`
				Sessions []modelSessionJSON `json:"sessions"`
			}{meta, toModelSummaryJSON(value.Summary), mapModelSessions(value.Sessions)}, "", "  ")
		}
		rows := make([]modelSummaryJSON, 0, len(model.Models))
		for _, row := range model.Models {
			rows = append(rows, toModelSummaryJSON(row))
		}
		return json.MarshalIndent(struct {
			queryJSONMeta
			Rows []modelSummaryJSON `json:"rows"`
		}{meta, rows}, "", "  ")
	case "skills":
		value, ok := model.SkillDetail(detailKey)
		if !ok {
			return nil, fmt.Errorf("skill detail %q was not found", detailKey)
		}
		return json.MarshalIndent(struct {
			queryJSONMeta
			Summary  skillSummaryJSON   `json:"summary"`
			Sessions []skillSessionJSON `json:"sessions"`
		}{meta, toSkillSummaryJSON(value.Summary), mapSkillSessions(value.Sessions)}, "", "  ")
	case "sessions":
		if detail {
			value, ok := model.SessionDetail(detailKey)
			if !ok {
				return nil, fmt.Errorf("session detail %q was not found", detailKey)
			}
			return json.MarshalIndent(struct {
				queryJSONMeta
				Summary sessionSummaryJSON `json:"summary"`
				Turns   []turnSummaryJSON  `json:"turns"`
			}{meta, toSessionSummaryJSON(value.Summary), mapTurnSummaries(value.Turns)}, "", "  ")
		}
		rows := make([]sessionSummaryJSON, 0, len(model.Sessions))
		for _, row := range model.Sessions {
			rows = append(rows, toSessionSummaryJSON(row))
		}
		return json.MarshalIndent(struct {
			queryJSONMeta
			Rows []sessionSummaryJSON `json:"rows"`
		}{meta, rows}, "", "  ")
	default:
		return nil, fmt.Errorf("unknown query report kind %q", kind)
	}
}

func WriteQueryJSON(w io.Writer, kind string, ctx ReportContext, model query.ReadModel, detailKey string) error {
	data, err := RenderQueryJSON(kind, ctx, model, detailKey)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

func toModelSummaryJSON(value query.ModelSummary) modelSummaryJSON {
	return modelSummaryJSON{Model: value.Model, Sessions: value.Sessions, Turns: value.Turns, UserPrompts: value.UserPrompts, ToolCalls: value.ToolCalls, SkillUses: value.SkillUses, TokenUsage: value.TokenUsage, TokenUsageAvailable: value.TokenUsageAvailable, FirstUsed: formatMachineTime(value.FirstUsed), LastUsed: formatMachineTime(value.LastUsed)}
}

func mapModelSessions(values []query.ModelSessionUsage) []modelSessionJSON {
	result := make([]modelSessionJSON, 0, len(values))
	for _, value := range values {
		result = append(result, modelSessionJSON{ID: value.ID, Title: value.Title, Source: value.Source, Agent: value.Agent, Project: value.Project, Turns: value.Turns, TokenUsage: value.TokenUsage, FirstUsed: formatMachineTime(value.FirstUsed), LastUsed: formatMachineTime(value.LastUsed)})
	}
	return result
}

func toSkillSummaryJSON(value query.SkillSummary) skillSummaryJSON {
	return skillSummaryJSON{Name: value.Name, Uses: value.Uses, Sessions: value.Sessions, Turns: value.Turns, Explicit: value.Explicit, Implicit: value.Implicit, Unknown: value.Unknown, Confirmed: value.Confirmed, Inferred: value.Inferred, Unconfirmed: value.Unconfirmed, ModeCounts: value.ModeCounts, StateCounts: value.StateCounts, MethodCounts: value.MethodCounts, FirstUsed: formatMachineTime(value.FirstUsed), LastUsed: formatMachineTime(value.LastUsed)}
}

func mapSkillSessions(values []query.SkillSessionUsage) []skillSessionJSON {
	result := make([]skillSessionJSON, 0, len(values))
	for _, value := range values {
		result = append(result, skillSessionJSON{SessionID: value.SessionID, Title: value.Title, Source: value.Source, Agent: value.Agent, Project: value.Project, Uses: value.Uses, Turns: value.Turns, ModeCounts: value.ModeCounts, StateCounts: value.StateCounts, MethodCounts: value.MethodCounts, FirstUsed: formatMachineTime(value.FirstUsed), LastUsed: formatMachineTime(value.LastUsed)})
	}
	return result
}

func toSessionSummaryJSON(value query.SessionSummary) sessionSummaryJSON {
	return sessionSummaryJSON{ID: value.ID, Title: value.Title, Source: value.Source, Agent: value.Agent, Provider: value.Provider, ProviderSessionID: value.ProviderSessionID, CtxSessionID: value.CtxSessionID, ProjectName: value.ProjectName, ProjectPath: value.ProjectPath, CLIVersion: value.CLIVersion, CreatedAt: formatMachineTime(value.CreatedAt), UpdatedAt: formatMachineTime(value.UpdatedAt), StartedAt: formatMachineTime(value.StartedAt), EndedAt: formatMachineTime(value.EndedAt), Models: value.Models, Turns: value.Turns, UserPrompts: value.UserPrompts, ToolCalls: value.ToolCalls, SkillUses: value.SkillUses, TokenUsage: value.TokenUsage, TokenUsageAvailable: value.TokenUsageAvailable, Aborted: value.Aborted}
}

func displayProject(name, path string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return path
}

func mapTurnSummaries(values []query.TurnSummary) []turnSummaryJSON {
	result := make([]turnSummaryJSON, 0, len(values))
	for _, value := range values {
		result = append(result, turnSummaryJSON{ID: value.ID, Ordinal: value.Ordinal, StartedAt: formatMachineTime(value.StartedAt), EndedAt: formatMachineTime(value.EndedAt), Status: queryTurnStatus(value), Models: value.Models, TokenUsage: value.TokenUsage, TokenUsageAvailable: value.TokenUsageAvailable, Tools: value.Tools, Skills: value.Skills})
	}
	return result
}
