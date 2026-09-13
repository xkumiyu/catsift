// Package tui contains the read-only terminal explorer. Bubble Tea is kept at
// this boundary; query and usage do not depend on terminal framework types.
package tui

import (
	"charm.land/lipgloss/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/xkumiyu/catsift/internal/output"
	"github.com/xkumiyu/catsift/internal/query"
	"github.com/xkumiyu/catsift/internal/usage"
	"golang.org/x/term"
)

var ErrNotInteractive = errors.New("catsift requires an interactive terminal")

type Route string

const (
	RouteOverview           Route = "overview"
	RouteModels             Route = "models"
	RouteSkills             Route = "skills"
	RouteSessions           Route = "sessions"
	RouteModelDetail        Route = "model-detail"
	RouteSkillDetail        Route = "skill-detail"
	RouteSessionDetail      Route = "session-detail"
	RouteTurnDetail         Route = "turn-detail"
	timeColumnWidth               = 18
	relativeTimeColumnWidth       = 12
)

type sortMode uint8

const (
	sortDefault sortMode = iota
	sortLastUsed
	sortName
	sortTurns
	sortFirstUsed
)

type sortOption struct {
	mode  sortMode
	label string
}

// ReloadFunc reloads the source snapshot. The current view remains visible
// until the returned input has been rebuilt into a read model.
type ReloadFunc func() (query.Input, error)

type RunOptions struct {
	Input       query.Input
	Filter      query.Filter
	SourcePath  string
	SourcePaths map[usage.SourceKind]string
	Reload      ReloadFunc
	Stdin       io.Reader
	Stdout      io.Writer
}

// Run starts the interactive program after verifying both streams are TTYs.
// Bubble Tea owns raw-mode setup and restoration through Program.Run.
func Run(options RunOptions) error {
	stdin := options.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := options.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	if !interactive(stdin, stdout) {
		return ErrNotInteractive
	}
	state := NewState(options.Input, options.Filter, options.Reload)
	state.sourcePath = options.SourcePath
	state.sourcePaths = cloneSourcePaths(options.SourcePaths)
	program := tea.NewProgram(state, tea.WithAltScreen(), tea.WithInput(stdin), tea.WithOutput(stdout))
	_, err := program.Run()
	return err
}

func interactive(stdin io.Reader, stdout io.Writer) bool {
	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	return inOK && outOK && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

// IsInteractive reports whether both streams are real terminals. The CLI
// uses this before loading history so a redirected invocation cannot emit an
// interactive screen or source data.
func IsInteractive(stdin io.Reader, stdout io.Writer) bool {
	return interactive(stdin, stdout)
}

// ReloadResultMsg is public so tests and embedding callers can deliver a
// completed reload without depending on Bubble Tea's command scheduler.
type ReloadResultMsg struct {
	Input query.Input
	Err   error
}

// State implements tea.Model and contains only selection, filter and safe
// read-model state. It has no source adapter or cache writer reference.
type State struct {
	Input       query.Input
	ReadModel   query.ReadModel
	Filter      query.Filter
	Route       Route
	ParentRoute Route
	Selected    int
	Offset      int
	Width       int
	Height      int
	Loading     bool
	Status      string

	searching         bool
	searchInput       string
	periodEditing     bool
	periodInput       string
	periodNotice      string
	selectedKey       string
	selectedTurnIndex int
	parentSelected    int
	parentOffset      int
	reload            ReloadFunc
	pendingReload     tea.Cmd
	help              bool
	sortMode          sortMode
	history           []navigationContext
	sourcePath        string
	sourcePaths       map[usage.SourceKind]string
	sourceFilterOpen  bool
	sourceSelected    int
	sourceOriginal    []usage.SourceKind
	now               time.Time
}

type navigationContext struct {
	route       Route
	selectedKey string
	selected    int
	offset      int
	search      string
	sortMode    sortMode
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	sectionStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	identityStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	infoStyle     = lipgloss.NewStyle().Faint(true)
	warningStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("24"))
)

const (
	overviewTwoColumnMinWidth = 100
	overviewColumnGap         = 3
)

func NewState(input query.Input, filter query.Filter, reload ReloadFunc) *State {
	input = query.SanitizeInput(input)
	if filter.Sources == nil {
		if filter.Source != "" {
			filter.Sources = []usage.SourceKind{filter.Source}
		} else {
			filter.Sources = inputSources(input)
		}
	}
	if filter.Source == "" && len(filter.Sources) == 1 {
		filter.Source = input.Source
	}
	state := &State{
		Input:             input,
		Filter:            filter,
		Route:             RouteOverview,
		Width:             100,
		Height:            30,
		selectedTurnIndex: -1,
		reload:            reload,
		now:               time.Now().UTC(),
	}
	state.rebuildReadModel()
	return state
}

func (state *State) Init() tea.Cmd { return nil }

func (state *State) rebuildReadModel() {
	filter := state.Filter
	filter.Search = ""
	state.ReadModel = query.Build(state.Input, filter)
}

func (state *State) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		state.Width, state.Height = message.Width, message.Height
		state.clampSelection()
		return state, nil
	case ReloadResultMsg:
		state.clearPeriodNotice()
		state.Loading = false
		state.pendingReload = nil
		if message.Err != nil {
			state.Status = "reload failed: " + compactError(message.Err)
			return state, nil
		}
		state.Input = query.SanitizeInput(message.Input)
		state.rebuildReadModel()
		state.Status = ""
		state.clampSelection()
		return state, nil
	case tea.KeyMsg:
		return state.updateKey(message)
	default:
		return state, nil
	}
}

func (state *State) updateKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if message.Type == tea.KeyCtrlC || message.String() == "ctrl+c" {
		return state, tea.Quit
	}
	if state.sourceFilterOpen {
		return state.updateSourceFilter(message)
	}
	if !state.searching && !state.periodEditing && message.String() == "q" {
		return state, tea.Quit
	}
	if state.help {
		switch message.String() {
		case "?", "esc":
			state.help = false
		}
		return state, nil
	}
	if state.searching {
		return state.updateSearch(message)
	}
	if state.periodEditing {
		return state.updatePeriod(message)
	}
	if message.Type == tea.KeyEscape || message.String() == "esc" {
		state.back()
		return state, nil
	}
	switch message.String() {
	case "?":
		state.help = true
	case "1":
		state.setRoute(RouteOverview)
	case "2":
		state.setRoute(RouteModels)
	case "3":
		state.setRoute(RouteSkills)
	case "4":
		state.setRoute(RouteSessions)
	case "/":
		if isSearchableRoute(state.Route) {
			state.searching = true
			state.searchInput = state.Filter.Search
		}
	case "d":
		state.periodEditing = true
		state.periodInput = ""
		state.Status = ""
		state.clearPeriodNotice()
	case "r":
		return state, state.startReload()
	case "o":
		state.openSourceFilter()
	case "s":
		if isSortableRoute(state.Route) {
			state.cycleSort()
		}
	case "c":
		state.Filter.Source = ""
		state.Filter.Sources = state.sourceOptions()
		state.Filter.Agent = ""
		state.Filter.Project = ""
		state.Filter.Model = usage.ModelRef{}
		state.Filter.ModelKey = ""
		state.Filter.ModelProvider = ""
		state.Filter.ModelName = ""
		state.Filter.Skill = ""
		state.Filter.Search = ""
		state.Filter.From = time.Time{}
		state.Filter.To = time.Time{}
		state.Status = ""
		state.clearPeriodNotice()
		state.rebuildReadModel()
		state.setRoute(RouteOverview)
	case "f":
		state.filterSelectedRow()
	case "a", "p":
		state.filterSelectedSession(message.String())
	case "b":
		state.back()
	case "tab", "l", "right":
		if !state.isDetail() {
			state.cycleRoute(1)
		}
	case "shift+tab", "h":
		if state.isDetail() {
			state.back()
		} else {
			state.cycleRoute(-1)
		}
	case "left":
		if !state.isDetail() {
			state.cycleRoute(-1)
		}
	case "up", "k":
		state.updateSelection(-1)
	case "down", "j":
		state.updateSelection(1)
	case "pgup":
		state.updateSelection(-state.visibleHeight())
	case "pgdown":
		state.updateSelection(state.visibleHeight())
	case "home":
		if state.Route == RouteOverview {
			state.scrollOverview(-state.rowCount())
		} else {
			state.Selected, state.Offset = 0, 0
		}
	case "end":
		if state.Route == RouteOverview {
			state.scrollOverview(state.rowCount())
		} else {
			state.Selected = state.rowCount() - 1
			state.clampSelection()
		}
	case "enter":
		state.openSelected()
	}
	return state, nil
}

func (state *State) openSourceFilter() {
	options := state.sourceOptions()
	if len(options) == 0 {
		state.Status = "source filter unavailable"
		return
	}
	state.sourceFilterOpen = true
	state.sourceSelected = 0
	state.sourceOriginal = append([]usage.SourceKind(nil), state.Filter.Sources...)
}

func (state *State) updateSourceFilter(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	options := state.sourceOptions()
	if len(options) == 0 {
		state.sourceFilterOpen = false
		return state, nil
	}
	switch message.String() {
	case "esc", "b":
		state.Filter.Sources = append([]usage.SourceKind(nil), state.sourceOriginal...)
		state.sourceFilterOpen = false
		state.sourceOriginal = nil
		state.rebuildReadModel()
		state.clampSelection()
	case "enter":
		state.sourceFilterOpen = false
		state.sourceOriginal = nil
	case "up", "k":
		if state.sourceSelected > 0 {
			state.sourceSelected--
		}
	case "down", "j":
		if state.sourceSelected < len(options)-1 {
			state.sourceSelected++
		}
	case " ":
		state.toggleSource(options[state.sourceSelected])
	case "a":
		state.Filter.Source = ""
		state.Filter.Sources = append([]usage.SourceKind(nil), options...)
		state.rebuildReadModel()
		state.clampSelection()
	case "n":
		state.Filter.Source = ""
		state.Filter.Sources = []usage.SourceKind{}
		state.rebuildReadModel()
		state.clampSelection()
	}
	return state, nil
}

func (state *State) toggleSource(source usage.SourceKind) {
	selected := append([]usage.SourceKind(nil), state.Filter.Sources...)
	state.Filter.Source = ""
	for index, value := range selected {
		if value != source {
			continue
		}
		selected = append(selected[:index], selected[index+1:]...)
		state.Filter.Sources = selected
		state.rebuildReadModel()
		state.clampSelection()
		return
	}
	selected = append(selected, source)
	state.Filter.Sources = orderedSources(selected)
	state.rebuildReadModel()
	state.clampSelection()
}

func (state *State) updateSearch(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.Type {
	case tea.KeyEscape:
		state.searching = false
		state.searchInput = ""
		return state, nil
	case tea.KeyEnter:
		state.Filter.Search = strings.TrimSpace(state.searchInput)
		state.searching = false
		state.Selected, state.Offset = 0, 0
		state.rebuildReadModel()
		return state, nil
	case tea.KeyBackspace:
		state.searchInput = trimLastRune(state.searchInput)
		return state, nil
	}
	if message.Type == tea.KeyRunes && len(message.Runes) > 0 {
		for _, value := range message.Runes {
			if utf8.ValidRune(value) && value >= ' ' && value != '\u007f' {
				state.searchInput += string(value)
			}
		}
	}
	return state, nil
}

func (state *State) updatePeriod(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.Type {
	case tea.KeyEscape:
		state.periodEditing = false
		state.periodInput = ""
		return state, nil
	case tea.KeyEnter:
		from, to, err := parsePeriodInput(state.periodInput, state.now)
		if err != nil {
			state.Status = "period: " + err.Error()
			return state, nil
		}
		state.clearPeriodNotice()
		state.Filter.From, state.Filter.To = from, to
		state.periodEditing = false
		state.periodInput = ""
		state.Status = ""
		state.Selected, state.Offset = 0, 0
		state.rebuildReadModel()
		state.setRoute(RouteOverview)
		state.periodNotice = state.periodNoticeText()
		return state, nil
	case tea.KeyBackspace:
		state.periodInput = trimLastRune(state.periodInput)
		return state, nil
	}
	if message.Type == tea.KeyRunes && len(message.Runes) > 0 {
		for _, value := range message.Runes {
			if utf8.ValidRune(value) && value >= ' ' && value != '\u007f' {
				state.periodInput += string(value)
			}
		}
	}
	return state, nil
}

func parsePeriodInput(value string, now time.Time) (time.Time, time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "all") {
		return time.Time{}, time.Time{}, nil
	}
	if days, err := strconv.Atoi(value); err == nil {
		if days <= 0 {
			return time.Time{}, time.Time{}, errors.New("period days must be at least 1")
		}
		const maxPeriodDays = int64((1<<63 - 1) / int64(24*time.Hour))
		if int64(days) > maxPeriodDays {
			return time.Time{}, time.Time{}, errors.New("period is too large")
		}
		if now.IsZero() {
			now = time.Now().UTC()
		}
		return now.Add(-time.Duration(days) * 24 * time.Hour), now, nil
	}
	if strings.Contains(value, "..") {
		bounds := strings.Split(value, "..")
		if len(bounds) != 2 || (strings.TrimSpace(bounds[0]) == "" && strings.TrimSpace(bounds[1]) == "") {
			return time.Time{}, time.Time{}, errors.New("period must be all, N, YYYY-MM-DD, or YYYY-MM-DD..YYYY-MM-DD")
		}
		from, err := parsePeriodDate(bounds[0])
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		to, err := parsePeriodDate(bounds[1])
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		if !to.IsZero() {
			to = to.AddDate(0, 0, 1)
		}
		if !from.IsZero() && !to.IsZero() && !from.Before(to) {
			return time.Time{}, time.Time{}, errors.New("period start must be before period end")
		}
		return from, to, nil
	}
	from, err := parsePeriodDate(value)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, from.AddDate(0, 0, 1), nil
}

func parsePeriodDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, time.UTC)
	if err != nil {
		return time.Time{}, errors.New("period dates must use YYYY-MM-DD")
	}
	return parsed, nil
}

func trimLastRune(value string) string {
	if value == "" {
		return value
	}
	_, size := utf8.DecodeLastRuneInString(value)
	return value[:len(value)-size]
}

func (state *State) startReload() tea.Cmd {
	if state.reload == nil {
		state.Status = "reload unavailable"
		return nil
	}
	if state.Loading {
		return nil
	}
	state.Loading = true
	command := func() tea.Msg {
		input, err := state.reload()
		return ReloadResultMsg{Input: input, Err: err}
	}
	state.pendingReload = command
	return command
}

func (state *State) setRoute(route Route) {
	state.clearPeriodNotice()
	state.Route = route
	state.ParentRoute = ""
	state.selectedKey = ""
	state.selectedTurnIndex = -1
	state.Filter.Search = ""
	state.Selected, state.Offset = 0, 0
	state.sortMode = sortDefault
	state.history = nil
	state.clampSelection()
}

func sortOptions(route Route) []sortOption {
	switch route {
	case RouteModels, RouteSkills:
		return []sortOption{{sortDefault, "Usage"}, {sortLastUsed, "Last Used"}, {sortName, "Name"}}
	case RouteSessions:
		return []sortOption{{sortDefault, "Last Used"}, {sortName, "Name"}, {sortTurns, "Turns"}}
	case RouteModelDetail:
		return []sortOption{{sortDefault, "Last Used"}, {sortTurns, "Turns"}, {sortName, "Name"}}
	case RouteSkillDetail:
		return []sortOption{{sortDefault, "Last Used"}, {sortFirstUsed, "First Used"}, {sortName, "Name"}}
	default:
		return nil
	}
}

func isSortableRoute(route Route) bool {
	return len(sortOptions(route)) > 0
}

func (state *State) sortLabel() string {
	for _, option := range sortOptions(state.Route) {
		if option.mode == state.sortMode {
			return option.label
		}
	}
	return ""
}

func (state *State) cycleSort() {
	options := sortOptions(state.Route)
	if len(options) < 2 {
		return
	}
	index := 0
	for i, option := range options {
		if option.mode == state.sortMode {
			index = i
			break
		}
	}
	state.sortMode = options[(index+1)%len(options)].mode
	state.Selected, state.Offset = 0, 0
	state.clampSelection()
}

func (state *State) cycleRoute(delta int) {
	routes := []Route{RouteOverview, RouteModels, RouteSkills, RouteSessions}
	current := state.topRoute()
	index := 0
	for i, route := range routes {
		if route == current {
			index = i
			break
		}
	}
	index = (index + delta) % len(routes)
	if index < 0 {
		index += len(routes)
	}
	state.setRoute(routes[index])
}

func (state *State) topRoute() Route {
	if !state.isDetail() {
		return state.Route
	}
	for i := len(state.history) - 1; i >= 0; i-- {
		if !isDetailRoute(state.history[i].route) {
			return state.history[i].route
		}
	}
	return state.ParentRoute
}

func isDetailRoute(route Route) bool {
	switch route {
	case RouteModelDetail, RouteSkillDetail, RouteSessionDetail, RouteTurnDetail:
		return true
	default:
		return false
	}
}

func isSearchableRoute(route Route) bool {
	switch route {
	case RouteModels, RouteSkills, RouteSessions:
		return true
	default:
		return false
	}
}

func (state *State) isDetail() bool {
	switch state.Route {
	case RouteModelDetail, RouteSkillDetail, RouteSessionDetail, RouteTurnDetail:
		return true
	default:
		return false
	}
}

func (state *State) openSelected() {
	if state.Selected < 0 {
		return
	}
	switch state.Route {
	case RouteModels:
		rows := state.filteredModels()
		if state.Selected >= len(rows) {
			return
		}
		state.push(RouteModelDetail, rows[state.Selected].Model.Key())
	case RouteSkills:
		rows := state.filteredSkills()
		if state.Selected >= len(rows) {
			return
		}
		state.push(RouteSkillDetail, rows[state.Selected].Name)
	case RouteSessions:
		rows := state.filteredSessions()
		if state.Selected >= len(rows) {
			return
		}
		state.push(RouteSessionDetail, rows[state.Selected].Key)
	case RouteModelDetail:
		rows := state.modelDetailRows()
		if state.Selected >= len(rows) {
			return
		}
		state.push(RouteSessionDetail, rows[state.Selected].Key)
	case RouteSkillDetail:
		rows := state.skillDetailRows()
		if state.Selected >= len(rows) {
			return
		}
		state.push(RouteSessionDetail, rows[state.Selected].SessionKey)
	case RouteSessionDetail:
		detail, ok := state.ReadModel.SessionDetail(state.selectedKey)
		if !ok || state.Selected >= len(detail.Turns) {
			return
		}
		turnIndex := state.Selected
		state.push(RouteTurnDetail, state.selectedKey)
		state.selectedTurnIndex = turnIndex
	default:
		return
	}
}

func (state *State) push(route Route, key string) {
	state.clearPeriodNotice()
	state.history = append(state.history, navigationContext{route: state.Route, selectedKey: state.selectedKey, selected: state.Selected, offset: state.Offset, search: state.Filter.Search, sortMode: state.sortMode})
	state.parentSelected, state.parentOffset = state.Selected, state.Offset
	state.ParentRoute = state.Route
	state.Route = route
	state.selectedKey = key
	state.selectedTurnIndex = -1
	state.Filter.Search = ""
	state.Selected, state.Offset = 0, 0
	state.sortMode = sortDefault
}

func (state *State) back() {
	if len(state.history) == 0 {
		return
	}
	state.clearPeriodNotice()
	last := len(state.history) - 1
	context := state.history[last]
	state.history = state.history[:last]
	state.Route = context.route
	state.selectedKey = context.selectedKey
	state.selectedTurnIndex = -1
	state.Filter.Search = context.search
	state.Selected, state.Offset = context.selected, context.offset
	state.sortMode = context.sortMode
	if len(state.history) == 0 {
		state.ParentRoute = ""
	} else {
		state.ParentRoute = state.history[len(state.history)-1].route
	}
	state.clampSelection()
}

func (state *State) filterSelectedRow() {
	switch state.Route {
	case RouteModels:
		rows := state.filteredModels()
		if state.Selected < len(rows) {
			state.Filter.Model = rows[state.Selected].Model
			state.Filter.ModelKey = ""
			state.Filter.ModelProvider = ""
			state.Filter.ModelName = ""
		}
	case RouteSkills:
		rows := state.filteredSkills()
		if state.Selected < len(rows) {
			state.Filter.Skill = rows[state.Selected].Name
		}
	default:
		return
	}
	state.Filter.Search = ""
	state.rebuildReadModel()
	state.setRoute(RouteOverview)
}

func (state *State) filterSelectedSession(field string) {
	rows := state.filteredSessions()
	if state.Selected >= len(rows) {
		return
	}
	switch field {
	case "a":
		state.Filter.Agent = rows[state.Selected].Agent
	case "p":
		state.Filter.Project = rows[state.Selected].ProjectPath
	default:
		return
	}
	state.Filter.Search = ""
	state.rebuildReadModel()
	state.setRoute(RouteOverview)
}

func (state *State) filteredModels() []query.ModelSummary {
	rows := state.ReadModel.Models
	if strings.TrimSpace(state.Filter.Search) != "" {
		rows = make([]query.ModelSummary, 0, len(state.ReadModel.Models))
		for _, row := range state.ReadModel.Models {
			if matchesSearch(state.Filter.Search, row.Model.Provider, row.Model.Name, row.Model.Provider+"/"+row.Model.Name) {
				rows = append(rows, row)
			}
		}
	}
	if state.sortMode == sortDefault {
		return rows
	}
	rows = append([]query.ModelSummary(nil), rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		switch state.sortMode {
		case sortLastUsed:
			if !rows[i].LastUsed.Equal(rows[j].LastUsed) {
				return rows[i].LastUsed.After(rows[j].LastUsed)
			}
			if rows[i].Turns != rows[j].Turns {
				return rows[i].Turns > rows[j].Turns
			}
			return rows[i].Model.Key() < rows[j].Model.Key()
		case sortName:
			left, right := strings.ToLower(rows[i].Model.Key()), strings.ToLower(rows[j].Model.Key())
			if left != right {
				return left < right
			}
			return rows[i].Model.Key() < rows[j].Model.Key()
		default:
			return false
		}
	})
	return rows
}

func (state *State) filteredSkills() []query.SkillSummary {
	rows := state.ReadModel.Skills
	if strings.TrimSpace(state.Filter.Search) != "" {
		rows = make([]query.SkillSummary, 0, len(state.ReadModel.Skills))
		for _, row := range state.ReadModel.Skills {
			if matchesSearch(state.Filter.Search, row.Name) {
				rows = append(rows, row)
			}
		}
	}
	if state.sortMode == sortDefault {
		return rows
	}
	rows = append([]query.SkillSummary(nil), rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		switch state.sortMode {
		case sortLastUsed:
			if !rows[i].LastUsed.Equal(rows[j].LastUsed) {
				return rows[i].LastUsed.After(rows[j].LastUsed)
			}
			if rows[i].Uses != rows[j].Uses {
				return rows[i].Uses > rows[j].Uses
			}
			return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
		case sortName:
			left, right := strings.ToLower(rows[i].Name), strings.ToLower(rows[j].Name)
			if left != right {
				return left < right
			}
			return rows[i].Name < rows[j].Name
		default:
			return false
		}
	})
	return rows
}

func (state *State) filteredSessions() []query.SessionSummary {
	rows := state.ReadModel.Sessions
	if strings.TrimSpace(state.Filter.Search) != "" {
		rows = make([]query.SessionSummary, 0, len(state.ReadModel.Sessions))
		for _, row := range state.ReadModel.Sessions {
			if matchesSearch(state.Filter.Search,
				row.Key,
				row.ID,
				string(row.Source),
				row.Agent,
				row.Provider,
				row.ProviderSessionID,
				row.CtxSessionID,
				row.ProjectPath,
				row.CLIVersion,
				row.Title,
				sessionLabel(row.Title, row.Source, row.Agent, row.ID, row.Aborted),
			) {
				rows = append(rows, row)
			}
		}
	}
	if state.sortMode == sortDefault {
		return rows
	}
	rows = append([]query.SessionSummary(nil), rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		switch state.sortMode {
		case sortName:
			left, right := strings.ToLower(sessionName(rows[i].Title, rows[i].ID)), strings.ToLower(sessionName(rows[j].Title, rows[j].ID))
			if left != right {
				return left < right
			}
			return rows[i].Key < rows[j].Key
		case sortTurns:
			if rows[i].Turns != rows[j].Turns {
				return rows[i].Turns > rows[j].Turns
			}
			if !rows[i].EndedAt.Equal(rows[j].EndedAt) {
				return rows[i].EndedAt.After(rows[j].EndedAt)
			}
			return rows[i].Key < rows[j].Key
		default:
			return false
		}
	})
	return rows
}

func (state *State) modelDetailRows() []query.ModelSessionUsage {
	detail, ok := state.ReadModel.ModelDetail(state.selectedKey)
	if !ok {
		return nil
	}
	if state.sortMode == sortDefault {
		return detail.Sessions
	}
	rows := append([]query.ModelSessionUsage(nil), detail.Sessions...)
	sort.SliceStable(rows, func(i, j int) bool {
		switch state.sortMode {
		case sortTurns:
			if rows[i].Turns != rows[j].Turns {
				return rows[i].Turns > rows[j].Turns
			}
			if !rows[i].LastUsed.Equal(rows[j].LastUsed) {
				return rows[i].LastUsed.After(rows[j].LastUsed)
			}
			return rows[i].Key < rows[j].Key
		case sortName:
			left, right := strings.ToLower(sessionName(rows[i].Title, rows[i].ID)), strings.ToLower(sessionName(rows[j].Title, rows[j].ID))
			if left != right {
				return left < right
			}
			return rows[i].Key < rows[j].Key
		default:
			return false
		}
	})
	return rows
}

func (state *State) skillDetailRows() []query.SkillSessionUsage {
	detail, ok := state.ReadModel.SkillDetail(state.selectedKey)
	if !ok {
		return nil
	}
	if state.sortMode == sortDefault {
		return detail.Sessions
	}
	rows := append([]query.SkillSessionUsage(nil), detail.Sessions...)
	sort.SliceStable(rows, func(i, j int) bool {
		switch state.sortMode {
		case sortFirstUsed:
			if !rows[i].FirstUsed.Equal(rows[j].FirstUsed) {
				return rows[i].FirstUsed.Before(rows[j].FirstUsed)
			}
			if !rows[i].LastUsed.Equal(rows[j].LastUsed) {
				return rows[i].LastUsed.After(rows[j].LastUsed)
			}
			return rows[i].SessionKey < rows[j].SessionKey
		case sortName:
			left, right := strings.ToLower(sessionName(rows[i].Title, rows[i].SessionID)), strings.ToLower(sessionName(rows[j].Title, rows[j].SessionID))
			if left != right {
				return left < right
			}
			return rows[i].SessionKey < rows[j].SessionKey
		default:
			return false
		}
	})
	return rows
}

func matchesSearch(search string, values ...string) bool {
	want := strings.ToLower(strings.TrimSpace(search))
	if want == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), want) {
			return true
		}
	}
	return false
}

func (state *State) rowCount() int {
	switch state.Route {
	case RouteOverview:
		return len(state.overviewLines())
	case RouteModels:
		return len(state.filteredModels())
	case RouteSkills:
		return len(state.filteredSkills())
	case RouteSessions:
		return len(state.filteredSessions())
	case RouteModelDetail:
		return len(state.modelDetailRows())
	case RouteSkillDetail:
		return len(state.skillDetailRows())
	case RouteSessionDetail:
		if detail, ok := state.ReadModel.SessionDetail(state.selectedKey); ok {
			return len(detail.Turns)
		}
	}
	return 0
}

func (state *State) canOpenSelected() bool {
	if state.Selected < 0 || state.Selected >= state.rowCount() {
		return false
	}
	switch state.Route {
	case RouteModels, RouteSkills, RouteSessions, RouteModelDetail, RouteSkillDetail, RouteSessionDetail:
		return true
	default:
		return false
	}
}

func (state *State) selectedRow(index int) bool {
	return index == state.Selected && state.canOpenSelected()
}

func (state *State) visibleHeight() int {
	value := state.frameBodyHeight() - state.routePrefixLines()
	if value < 1 {
		return 1
	}
	return value
}

func (state *State) frameBodyHeight() int {
	value := state.Height - len(state.headerLines()) - len(state.footerLines())
	if value < 1 {
		return 1
	}
	return value
}

func (state *State) routePrefixLines() int {
	switch state.Route {
	case RouteModels, RouteSkills, RouteSessions:
		return 3
	case RouteModelDetail, RouteSkillDetail:
		return 6
	case RouteSessionDetail:
		return state.sessionDetailPrefixLines()
	case RouteTurnDetail:
		return 0
	default:
		return 0
	}
}

func (state *State) sessionDetailPrefixLines() int {
	lines := 9
	if detail, ok := state.ReadModel.SessionDetail(state.selectedKey); ok {
		if state.Width >= 100 && showSessionIdentityMetadata(detail.Summary) {
			lines++
		}
		if detail.Summary.Aborted {
			lines++
		}
	}
	return lines
}

func (state *State) updateSelection(delta int) {
	if state.Route == RouteOverview {
		state.scrollOverview(delta)
		return
	}
	count := state.rowCount()
	if count == 0 {
		state.Selected, state.Offset = 0, 0
		return
	}
	state.Selected += delta
	state.clampSelection()
	visible := state.visibleHeight()
	if state.Selected < state.Offset {
		state.Offset = state.Selected
	}
	if state.Selected >= state.Offset+visible {
		state.Offset = state.Selected - visible + 1
	}
	if state.Offset < 0 {
		state.Offset = 0
	}
}

func (state *State) scrollOverview(delta int) {
	count := state.rowCount()
	if count == 0 {
		state.Selected, state.Offset = 0, 0
		return
	}
	maxOffset := count - state.visibleHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	state.Offset += delta
	if state.Offset < 0 {
		state.Offset = 0
	}
	if state.Offset > maxOffset {
		state.Offset = maxOffset
	}
	state.Selected = state.Offset
}

func (state *State) clampSelection() {
	count := state.rowCount()
	if count == 0 {
		state.Selected, state.Offset = 0, 0
		return
	}
	if state.Selected < 0 {
		state.Selected = 0
	}
	if state.Selected >= count {
		state.Selected = count - 1
	}
	if state.Offset < 0 {
		state.Offset = 0
	}
	if state.Offset > state.Selected {
		state.Offset = state.Selected
	}
	visible := state.visibleHeight()
	if state.Offset+visible > count {
		state.Offset = count - visible
		if state.Offset < 0 {
			state.Offset = 0
		}
	}
}

func (state *State) View() string {
	if state.help {
		return state.viewHelp()
	}
	if state.sourceFilterOpen {
		return state.viewSourceFilter()
	}
	top := state.headerLines()
	footer := state.footerLines()
	bodyHeight := state.Height - len(top) - len(footer)
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	body := fitBody(state.viewRoute(bodyHeight), bodyHeight)
	lines := make([]string, 0, len(top)+len(body)+len(footer))
	lines = append(lines, top...)
	lines = append(lines, body...)
	lines = append(lines, footer...)
	return boundView(lines, state.Width, state.Height)
}

func (state *State) viewSourceFilter() string {
	width := state.renderWidth()
	options := state.sourceOptions()
	lines := []string{
		titleStyle.Render(truncate("catsift / Sources", width)),
		"",
		sectionStyle.Render("Toggle sources"),
		mutedStyle.Render(truncate("Space toggle   a all   n none   Enter apply   Esc cancel", width)),
		"",
	}
	for index, source := range options {
		mark := "[ ]"
		if state.sourceEnabled(source) {
			mark = "[x]"
		}
		line := fmt.Sprintf("  %s %s", mark, output.FormatSourceContext(source, state.pathForSource(source)))
		line = truncate(line, width)
		if index == state.sourceSelected {
			line = selectedStyle.Render(padRightDisplay(line, width))
		}
		lines = append(lines, line)
	}
	return boundView(lines, width, state.Height)
}

func (state *State) headerLines() []string {
	width := state.renderWidth()
	lines := []string{titleStyle.Render(truncate("catsift", width))}
	view := state.ReadModel.Overview
	sourceField := metadataField{label: "Source", value: state.sourceContext(view.Source)}
	agentsField := metadataField{label: "Agents", value: formatAgents(view.Agents, view.Agent)}
	periodField := metadataField{label: "Period", value: formatPeriod(view.Period.From, view.Period.To)}
	scope := metadataLine("", sourceField, agentsField)
	period := metadataLine("", periodField)
	compactMetadata := metadataLine("", sourceField, agentsField, periodField)
	if lipgloss.Width(compactMetadata) <= width {
		lines = append(lines, compactMetadata)
	} else {
		lines = append(lines, scope, period)
	}
	lines = append(lines, state.tabsLine())
	if state.Loading {
		lines = append(lines, infoStyle.Render(truncate("Reloading snapshot…", width)))
	} else if state.Status != "" {
		lines = append(lines, warningStyle.Render(truncate(state.Status, width)))
	} else if state.periodNotice != "" {
		lines = append(lines, state.periodNoticeLines(width)...)
	}
	if state.periodEditing {
		lines = append(lines, truncate("Set period: "+state.periodInput+"_  Enter apply  Esc cancel", width))
	} else if state.searching {
		lines = append(lines, truncate("Search rows: /"+state.searchInput+"_  Enter apply  Esc cancel", width))
	}
	lines = append(lines, mutedStyle.Render(strings.Repeat("-", width)))
	return lines
}

func (state *State) sourceContext(fallback usage.SourceKind) string {
	sources := state.Filter.Sources
	if sources == nil && fallback != "" {
		sources = []usage.SourceKind{fallback}
	}
	if len(sources) == 0 {
		return "none"
	}
	values := make([]string, 0, len(sources))
	for _, source := range orderedSources(sources) {
		values = append(values, output.FormatSourceContext(source, state.pathForSource(source)))
	}
	return strings.Join(values, ", ")
}

func (state *State) pathForSource(source usage.SourceKind) string {
	if path := state.sourcePaths[source]; path != "" {
		return path
	}
	if source == state.Input.Source || len(state.sourceOptions()) == 1 {
		return state.sourcePath
	}
	return ""
}

func (state *State) periodNoticeLines(width int) []string {
	const label = "Notice: "
	notice := strings.TrimSpace(state.periodNotice)
	content := wrapNoticeWords(notice, maxInt(1, width-lipgloss.Width(label)), maxInt(1, width))
	lines := make([]string, 0, len(content))
	lines = append(lines, infoStyle.Render(truncate(label+content[0], width)))
	for _, line := range content[1:] {
		lines = append(lines, infoStyle.Render(truncate(line, width)))
	}
	return lines
}

func wrapNoticeWords(value string, firstWidth, nextWidth int) []string {
	words := strings.Fields(safeDisplay(value))
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, len(words))
	line := words[0]
	width := firstWidth
	for _, word := range words[1:] {
		candidate := line + " " + word
		if lipgloss.Width(candidate) <= width {
			line = candidate
			continue
		}
		lines = append(lines, line)
		line = word
		width = nextWidth
	}
	return append(lines, line)
}

func (state *State) tabsLine() string {
	width := state.renderWidth()
	routes := []struct {
		key   string
		route Route
		name  string
	}{
		{key: "1", route: RouteOverview, name: "Overview"},
		{key: "2", route: RouteModels, name: "Models"},
		{key: "3", route: RouteSkills, name: "Skills"},
		{key: "4", route: RouteSessions, name: "Sessions"},
	}
	parts := make([]string, 0, len(routes))
	active := state.topRoute()
	for _, item := range routes {
		part := fmt.Sprintf("[%s] %s", item.key, item.name)
		parts = append(parts, part)
	}
	raw := strings.Join(parts, "  ")
	if lipgloss.Width(raw) > width {
		parts = []string{"[1] Ovr", "[2] Mdl", "[3] Skl", "[4] Ses"}
		for i, item := range routes {
			if item.route == active {
				parts[i] = "[" + item.key + "] *"
			}
		}
		raw = strings.Join(parts, " ")
		if lipgloss.Width(raw) > width {
			return truncate(raw, width)
		}
	}
	for i, item := range routes {
		if item.route == active {
			parts[i] = sectionStyle.Render(parts[i])
		}
	}
	return strings.Join(parts, "  ")
}

func (state *State) footerLines() []string {
	var value string
	switch {
	case state.searching:
		value = "Enter apply   Esc cancel"
	case state.Route == RouteTurnDetail:
		value = "b/Esc back   1-4 switch   ? help   q quit"
	case state.isDetail():
		value = "j/k scroll   b/Esc back   1-4 switch   ? help   q quit"
		if isSortableRoute(state.Route) {
			value = "j/k scroll   s sort   b/Esc back   1-4 switch   ? help   q quit"
		}
		if state.canOpenSelected() {
			value = "j/k move   Enter open   b/Esc back   1-4 switch   ? help   q quit"
			if isSortableRoute(state.Route) {
				value = "j/k move   Enter open   s sort   b/Esc back   1-4 switch   ? help   q quit"
			}
		}
	case state.Route == RouteOverview:
		value = "j/k scroll   Home/End jump   d period   1-4 switch   o sources   r reload   ? help   q quit"
	case state.Route == RouteModels || state.Route == RouteSkills:
		value = "j/k move   / search rows   s sort   f filter   d period   o sources   r reload   ? help   q quit"
		if state.canOpenSelected() {
			value = "j/k move   Enter detail   / search rows   s sort   f filter   d period   o sources   r reload   ? help   q quit"
		}
	case state.Route == RouteSessions:
		value = "j/k move   / search rows   s sort   a agent   p project   d period   o sources   r reload   ? help   q quit"
		if state.canOpenSelected() {
			value = "j/k move   Enter detail   / search rows   s sort   a agent   p project   d period   o sources   r reload   ? help   q quit"
		}
	default:
		value = "j/k move   / search rows   d period   o sources   r reload   ? help   q quit"
		if state.canOpenSelected() {
			value = "j/k move   Enter detail   / search rows   d period   o sources   r reload   ? help   q quit"
		}
	}
	return []string{
		mutedStyle.Render(strings.Repeat("-", state.renderWidth())),
		mutedStyle.Render(truncate(value, state.renderWidth())),
	}
}

func (state *State) viewHelp() string {
	width := state.renderWidth()
	lines := []string{
		titleStyle.Render(truncate("catsift / Help", width)),
		"",
		sectionStyle.Render("Navigation"),
		"  1-4       switch Overview, Models, Skills, Sessions",
		"  Tab       next view",
		"  Shift+Tab previous view",
		"  j/k       move selection; scroll Overview",
		"  Home/End  jump to beginning/end",
		"  PgUp/PgDn scroll one page",
		"  Enter     open detail",
		"  b/Esc     go back",
		"",
		sectionStyle.Render("Actions"),
		"  /         search rows by safe metadata",
		"  f         filter selected model or skill",
		"  a         filter Sessions by selected agent",
		"  p         filter Sessions by selected project",
		"  o         toggle source visibility",
		"  d         set period: all, N, YYYY-MM-DD[..YYYY-MM-DD]",
		"  r         reload snapshot",
		"  s         cycle list sort",
		"  c         clear TUI filters",
		"  ?/Esc     close help",
		"  q/Ctrl+C  quit",
		"",
		mutedStyle.Render(truncate("Prompt text, tool arguments, and skill bodies are never displayed.", width)),
	}
	return boundView(lines, width, state.Height)
}

func (state *State) viewRoute(height int) []string {
	switch state.Route {
	case RouteOverview:
		return state.viewOverview(height)
	case RouteModels:
		return state.viewModels(height)
	case RouteSkills:
		return state.viewSkills(height)
	case RouteSessions:
		return state.viewSessions(height)
	case RouteModelDetail:
		return state.viewModelDetail(height)
	case RouteSkillDetail:
		return state.viewSkillDetail(height)
	case RouteSessionDetail:
		return state.viewSessionDetail(height)
	case RouteTurnDetail:
		return state.viewTurnDetail(height)
	default:
		return []string{"No view available."}
	}
}

func (state *State) viewOverview(height int) []string {
	lines := state.overviewLines()
	if height <= 0 || len(lines) <= height {
		return lines
	}
	start := state.Offset
	if start < 0 {
		start = 0
	}
	maxStart := len(lines) - height
	if start > maxStart {
		start = maxStart
	}
	return lines[start : start+height]
}

func (state *State) overviewLines() []string {
	view := state.ReadModel.Overview
	width := state.renderWidth()
	lines := []string{titleStyle.Render("Overview"), ""}
	activityLines := overviewActivityLines(view, width)
	tokenLines := overviewTokenLines(view, width)
	if width >= overviewTwoColumnMinWidth {
		lines = append(lines, joinOverviewColumns(activityLines, tokenLines, width)...)
	} else {
		lines = append(lines, activityLines...)
		lines = append(lines, "")
		lines = append(lines, tokenLines...)
	}

	if len(view.Trend) > 0 {
		lines = append(lines, "", sectionStyle.Render("Daily activity"))
		maxTurns := 1
		for _, point := range view.Trend {
			if point.Turns > maxTurns {
				maxTurns = point.Turns
			}
		}
		barWidth := trendBarWidth(width)
		lines = append(lines, renderTableHeader(width, trendCells(width, nil, "")))
		for _, point := range view.Trend {
			barLength := point.Turns * barWidth / maxTurns
			if point.Turns > 0 && barLength == 0 {
				barLength = 1
			}
			bar := strings.Repeat("#", barLength)
			lines = append(lines, renderTableRow(width, false, trendCells(width, &point, bar)))
		}
	}
	periodInfo := state.periodInfoLines()
	for _, group := range []struct {
		level string
		label string
		style lipgloss.Style
	}{
		{level: "warning", label: "Warnings", style: warningStyle},
		{level: "info", label: "Input notes", style: infoStyle},
	} {
		summaries := warningSummaries(state.ReadModel.Warnings, group.level)
		if len(summaries) == 0 && (group.level != "info" || len(periodInfo) == 0) {
			continue
		}
		lines = append(lines, "", group.style.Render(group.label))
		for _, summary := range summaries {
			lines = append(lines, group.style.Render(truncate("  "+warningSummaryText(summary), width)))
		}
		if group.level == "info" {
			for _, info := range periodInfo {
				lines = append(lines, infoStyle.Render(truncate("  "+info, width)))
			}
		}
	}
	return lines
}

type warningSummary struct {
	reason string
	count  int
}

func warningSummaries(warnings []usage.Warning, level string) []warningSummary {
	result := make([]warningSummary, 0, len(warnings))
	indexes := make(map[string]int, len(warnings))
	for _, warning := range warnings {
		if usage.WarningDiagnosticLevel(warning) != level {
			continue
		}
		reason := strings.TrimSpace(warning.Reason)
		if reason == "" {
			reason = "unknown"
		}
		if index, ok := indexes[reason]; ok {
			result[index].count += normalizedCount(warning.Count)
			continue
		}
		indexes[reason] = len(result)
		result = append(result, warningSummary{reason: reason, count: normalizedCount(warning.Count)})
	}
	return result
}

func warningSummaryText(summary warningSummary) string {
	count := summary.count
	singular, plural := "input issue", "input issues"
	switch summary.reason {
	case "large_line":
		singular, plural = "oversized history record skipped", "oversized history records skipped"
	case "empty_line":
		singular, plural = "empty history line skipped", "empty history lines skipped"
	case "malformed_json":
		singular, plural = "malformed JSON record skipped", "malformed JSON records skipped"
	case "ctx_malformed_json":
		singular, plural = "malformed ctx event record skipped", "malformed ctx event records skipped"
	case "ctx_invalid_event":
		singular, plural = "invalid ctx event skipped", "invalid ctx events skipped"
	case "ctx_unknown_record":
		singular, plural = "unknown ctx stream record skipped", "unknown ctx stream records skipped"
	case "ctx_unknown_event":
		singular, plural = "unknown ctx event skipped", "unknown ctx events skipped"
	case "ctx_invalid_timestamp":
		singular, plural = "ctx event has an invalid timestamp", "ctx events have invalid timestamps"
	case "ctx_missing_agent":
		singular, plural = "ctx event has no agent identity", "ctx events have no agent identity"
	case "unknown_type":
		singular, plural = "unknown record type skipped", "unknown record types skipped"
	case "invalid_timestamp":
		singular, plural = "record has an invalid timestamp", "records have invalid timestamps"
	case "read_file":
		singular, plural = "file could not be read", "files could not be read"
	case "opencode_invalid_session":
		singular, plural = "invalid OpenCode session skipped", "invalid OpenCode sessions skipped"
	case "opencode_malformed_message":
		singular, plural = "malformed OpenCode message skipped", "malformed OpenCode messages skipped"
	case "opencode_orphan_part":
		singular, plural = "orphan OpenCode part skipped", "orphan OpenCode parts skipped"
	case "opencode_malformed_part":
		singular, plural = "malformed OpenCode part skipped", "malformed OpenCode parts skipped"
	case "opencode_unknown_part":
		singular, plural = "unknown OpenCode part skipped", "unknown OpenCode parts skipped"
	case "cannot read skill inventory path":
		singular, plural = "skill inventory path could not be read", "skill inventory paths could not be read"
	default:
		if description := usage.WarningDescription(summary.reason); description != "" {
			return fmt.Sprintf("%d %s", count, description)
		}
	}
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}

func overviewActivityLines(view query.OverviewView, width int) []string {
	metrics := []overviewMetricSpec{
		{label: "Sessions", value: formatInt(view.Sessions)},
		{label: "Turns", value: formatInt(view.Turns)},
		{label: "User Prompts", value: formatInt(view.UserPrompts)},
		{label: "Tool Calls", value: formatInt(view.ToolCalls)},
		{label: "By turn", value: formatInt(view.SkillUses)},
		{label: "By session", value: formatInt(view.SkillUsesSession)},
	}
	metricLines := overviewMetricLines(metrics, width)
	lines := []string{sectionStyle.Render("Activity")}
	lines = append(lines, metricLines[:4]...)
	lines = append(lines, "", sectionStyle.Render("Skill Usage"))
	return append(lines, metricLines[4:]...)
}

func overviewTokenLines(view query.OverviewView, width int) []string {
	metrics := make([]overviewMetricSpec, 0, 6)
	if !view.TokenUsageAvailable && view.TokenUsage == (usage.TokenUsage{}) {
		metrics = append(metrics, overviewMetricSpec{label: "Status", value: "not available"})
	} else {
		metrics = append(metrics,
			overviewMetricSpec{label: "Total Tokens", value: formatTokenTotal(view.TokenUsage, true)},
			overviewMetricSpec{label: "Input Tokens", value: formatCompactInt(view.TokenUsage.InputTokens), indent: 1},
			overviewMetricSpec{label: "Cached Tokens", value: formatCompactInt(view.TokenUsage.CachedInputTokens), indent: 2},
		)
		if view.TokenUsage.CacheWriteInputTokens != 0 {
			metrics = append(metrics, overviewMetricSpec{label: "Cache Write Input Tokens", value: formatCompactInt(view.TokenUsage.CacheWriteInputTokens), indent: 2})
		}
		metrics = append(metrics,
			overviewMetricSpec{label: "Output Tokens", value: formatCompactInt(view.TokenUsage.OutputTokens), indent: 1},
			overviewMetricSpec{label: "Reasoning Tokens", value: formatCompactInt(view.TokenUsage.ReasoningOutputTokens), indent: 2},
		)
	}
	return append([]string{sectionStyle.Render("Token Usage")}, overviewMetricLines(metrics, width)...)
}

type overviewMetricSpec struct {
	label  string
	value  string
	indent int
}

func overviewMetricLines(metrics []overviewMetricSpec, width int) []string {
	labelWidth, valueWidth := 0, 0
	for _, metric := range metrics {
		label := strings.Repeat("  ", metric.indent) + metric.label
		labelWidth = maxInt(labelWidth, lipgloss.Width(label))
		valueWidth = maxInt(valueWidth, lipgloss.Width(metric.value))
	}
	lines := make([]string, len(metrics))
	for index, metric := range metrics {
		lines[index] = overviewMetric(metric.label, metric.value, metric.indent, labelWidth, valueWidth, width)
	}
	return lines
}

func joinOverviewColumns(left, right []string, width int) []string {
	leftWidth := (width - overviewColumnGap) / 2
	rightWidth := width - overviewColumnGap - leftWidth
	lineCount := maxInt(len(left), len(right))
	lines := make([]string, 0, lineCount)
	for i := 0; i < lineCount; i++ {
		leftValue, rightValue := "", ""
		if i < len(left) {
			leftValue = left[i]
		}
		if i < len(right) {
			rightValue = right[i]
		}
		lines = append(lines,
			padRightDisplay(leftValue, leftWidth)+strings.Repeat(" ", overviewColumnGap)+padRightDisplay(rightValue, rightWidth),
		)
	}
	return lines
}

func (state *State) viewModels(height int) []string {
	rows := state.filteredModels()
	width := state.renderWidth()
	lines := []string{primaryListHeading("Models ["+state.sortLabel()+"]", len(rows), state.Selected, width)}
	if len(rows) == 0 {
		if strings.TrimSpace(state.Filter.Search) != "" {
			return append(lines, mutedStyle.Render("No matching rows."))
		}
		return append(lines, mutedStyle.Render("No models in this scope."))
	}
	lines = append(lines, renderTableHeader(width, modelCells(width, nil)))
	rowHeight := height - len(lines)
	if rowHeight < 1 {
		rowHeight = 1
	}
	start, end := window(len(rows), state.Offset, rowHeight)
	for i := start; i < end; i++ {
		lines = append(lines, renderTableRow(width, state.selectedRow(i), modelCellsForRow(width, rows[i])))
	}
	return fitBody(lines, height)
}

func (state *State) viewSkills(height int) []string {
	rows := state.filteredSkills()
	width := state.renderWidth()
	lines := []string{primaryListHeading("Skills ["+state.sortLabel()+"]", len(rows), state.Selected, width)}
	if len(rows) == 0 {
		if strings.TrimSpace(state.Filter.Search) != "" {
			return append(lines, mutedStyle.Render("No matching rows."))
		}
		return append(lines, mutedStyle.Render("No skills in this scope."))
	}
	lines = append(lines, renderTableHeader(width, skillCells(width, nil)))
	rowHeight := height - len(lines)
	if rowHeight < 1 {
		rowHeight = 1
	}
	start, end := window(len(rows), state.Offset, rowHeight)
	for i := start; i < end; i++ {
		lines = append(lines, renderTableRow(width, state.selectedRow(i), skillCellsForRow(width, rows[i])))
	}
	return fitBody(lines, height)
}

func (state *State) viewSessions(height int) []string {
	rows := state.filteredSessions()
	width := state.renderWidth()
	lines := []string{primaryListHeading("Sessions ["+state.sortLabel()+"]", len(rows), state.Selected, width)}
	if len(rows) == 0 {
		if strings.TrimSpace(state.Filter.Search) != "" {
			return append(lines, mutedStyle.Render("No matching rows."))
		}
		return append(lines, mutedStyle.Render("No sessions in this scope."))
	}
	lines = append(lines, renderTableHeader(width, sessionCells(width, nil)))
	rowHeight := height - len(lines)
	if rowHeight < 1 {
		rowHeight = 1
	}
	start, end := window(len(rows), state.Offset, rowHeight)
	for i := start; i < end; i++ {
		lines = append(lines, renderTableRow(width, state.selectedRow(i), sessionCellsForRow(width, rows[i])))
	}
	return fitBody(lines, height)
}

func (state *State) viewModelDetail(height int) []string {
	detail, ok := state.ReadModel.ModelDetail(state.selectedKey)
	if !ok {
		return []string{"Model detail unavailable."}
	}
	rows := state.modelDetailRows()
	width := state.renderWidth()
	lines := []string{
		titleStyle.Render(truncate("Model detail", width)),
		identityStyle.Render(truncate("  Model: "+detail.Summary.Model.Provider+"/"+detail.Summary.Model.Name, width)),
		metadataLine("  ", metadataField{label: "Turns", value: formatInt(detail.Summary.Turns)}, metadataField{label: "Prompts", value: formatInt(detail.Summary.UserPrompts)}, metadataField{label: "Tools", value: formatInt(detail.Summary.ToolCalls)}, metadataField{label: "Skills", value: formatInt(detail.Summary.SkillUses)}),
		metadataLine("  ", metadataField{label: "Tokens", value: formatTokenTotal(detail.Summary.TokenUsage, detail.Summary.TokenUsageAvailable)}, metadataField{label: "First", value: formatTime(detail.Summary.FirstUsed)}, metadataField{label: "Last", value: formatTime(detail.Summary.LastUsed)}),
		listHeading("Sessions ["+state.sortLabel()+"]", len(rows), state.Selected, width),
		renderTableHeader(width, modelSessionCells(width, nil)),
	}
	rowHeight := height - len(lines)
	if rowHeight < 1 {
		rowHeight = 1
	}
	start, end := window(len(rows), state.Offset, rowHeight)
	for i := start; i < end; i++ {
		lines = append(lines, renderTableRow(width, state.selectedRow(i), modelSessionCellsForRow(width, rows[i])))
	}
	return fitBody(lines, height)
}

func (state *State) viewSkillDetail(height int) []string {
	detail, ok := state.ReadModel.SkillDetail(state.selectedKey)
	if !ok {
		return []string{"Skill detail unavailable."}
	}
	rows := state.skillDetailRows()
	row := detail.Summary
	width := state.renderWidth()
	lines := []string{
		titleStyle.Render(truncate("Skill detail", width)),
		identityStyle.Render(truncate("  Skill: "+row.Name, width)),
		truncate(fmt.Sprintf("  Turns with skill %d", row.Turns), width),
		metadataLine("  ", metadataField{label: "Usage mode", value: formatLabeledCounts(
			labeledCount{label: "Explicit", value: row.Explicit},
			labeledCount{label: "Implicit", value: row.Implicit},
			labeledCount{label: "Unknown", value: row.Unknown},
		)}, metadataField{label: "Evidence", value: formatLabeledCounts(
			labeledCount{label: "Confirmed", value: row.Confirmed},
			labeledCount{label: "Inferred", value: row.Inferred},
			labeledCount{label: "Unconfirmed", value: row.Unconfirmed},
		)}),
		listHeading("Sessions ["+state.sortLabel()+"]", len(rows), state.Selected, width),
		renderTableHeader(width, skillSessionCells(width, nil)),
	}
	rowHeight := height - len(lines)
	if rowHeight < 1 {
		rowHeight = 1
	}
	start, end := window(len(rows), state.Offset, rowHeight)
	for i := start; i < end; i++ {
		lines = append(lines, renderTableRow(width, state.selectedRow(i), skillSessionCellsForRow(width, rows[i])))
	}
	return fitBody(lines, height)
}

func (state *State) viewSessionDetail(height int) []string {
	detail, ok := state.ReadModel.SessionDetail(state.selectedKey)
	if !ok {
		return []string{"Session detail unavailable."}
	}
	row := detail.Summary
	width := state.renderWidth()
	lines := []string{
		titleStyle.Render(truncate("Session detail", width)),
		identityStyle.Render(truncate("  Session name: "+emptyDash(row.Title), width)),
		mutedStyle.Render(truncate("  Session ID: "+row.ID, width)),
	}
	if row.Aborted {
		lines = append(lines, mutedStyle.Render(truncate("  Status: aborted", width)))
	}
	if width >= 100 {
		metadata := []string{metadataLine("  ", metadataField{label: "Provider", value: emptyDash(row.Provider)})}
		if showSessionIdentityMetadata(row) {
			fields := make([]metadataField, 0, 2)
			if strings.TrimSpace(row.ProviderSessionID) != "" {
				fields = append(fields, metadataField{label: "Provider session", value: row.ProviderSessionID})
			}
			if strings.TrimSpace(row.CtxSessionID) != "" {
				fields = append(fields, metadataField{label: "ctx session", value: row.CtxSessionID})
			}
			metadata = append(metadata, metadataLine("  ", fields...))
		}
		metadata = append(metadata,
			metadataLine("  ", metadataField{label: "Created", value: formatTime(row.CreatedAt)}, metadataField{label: "Updated", value: formatTime(row.UpdatedAt)}),
			metadataLine("  ", metadataField{label: "Project", value: emptyDash(row.ProjectPath)}),
		)
		lines = append(lines, metadata...)
	} else {
		lines = append(lines,
			metadataLine("  ", metadataField{label: "Provider", value: emptyDash(row.Provider)}),
			metadataLine("  ", metadataField{label: "Project", value: emptyDash(row.ProjectPath)}),
			metadataLine("  ", metadataField{label: "Period", value: formatTime(row.StartedAt) + " to " + formatTime(row.EndedAt)}),
		)
	}
	lines = append(lines,
		metadataLine("  ", metadataField{label: "Prompts", value: formatInt(row.UserPrompts)}, metadataField{label: "Tools", value: formatInt(row.ToolCalls)}, metadataField{label: "Skills", value: formatInt(row.SkillUses)}, metadataField{label: "Tokens", value: formatTokenTotal(row.TokenUsage, row.TokenUsageAvailable)}),
		sectionStyle.Render(truncate("Turns", width)),
		renderTableHeader(width, turnCellsForSession(width, nil, detail.Turns)),
	)
	rowHeight := height - len(lines)
	if rowHeight < 1 {
		rowHeight = 1
	}
	start, end := window(len(detail.Turns), state.Offset, rowHeight)
	for i := start; i < end; i++ {
		turn := detail.Turns[i]
		lines = append(lines, renderTableRow(width, state.selectedRow(i), turnCellsForSession(width, &turn, detail.Turns)))
	}
	return fitBody(lines, height)
}

func (state *State) viewTurnDetail(height int) []string {
	detail, turn, ok := state.selectedTurnDetail()
	if !ok {
		return []string{"Turn detail unavailable."}
	}
	row := detail.Summary
	width := state.renderWidth()
	lines := []string{
		titleStyle.Render(truncate("Turn detail", width)),
		identityStyle.Render(truncate("  Turn: "+turn.ID, width)),
		mutedStyle.Render(truncate("  Session: "+sessionDisplayName(row.Title, row.ID), width)),
		mutedStyle.Render(truncate("  Session ID: "+row.ID, width)),
		metadataLine("  ", metadataField{label: "Model", value: emptyDash(modelNames(turn.Models))}, metadataField{label: "Status", value: turnStatus(&turn)}),
		metadataLine("  ", metadataField{label: "Started", value: formatTime(turn.StartedAt)}, metadataField{label: "Ended", value: formatTime(turn.EndedAt)}),
		metadataLine("  ", metadataField{label: "Tokens", value: formatTokenTotal(turn.TokenUsage, turn.TokenUsageAvailable)}),
		metadataLine("  ", metadataField{label: "Tools", value: formatTurnItems(turn.Tools)}),
		metadataLine("  ", metadataField{label: "Skills", value: formatTurnItems(turn.Skills)}),
	}
	return fitBody(lines, height)
}

func (state *State) selectedTurnDetail() (query.SessionDetail, query.TurnSummary, bool) {
	detail, ok := state.ReadModel.SessionDetail(state.selectedKey)
	if !ok || state.selectedTurnIndex < 0 || state.selectedTurnIndex >= len(detail.Turns) {
		return query.SessionDetail{}, query.TurnSummary{}, false
	}
	return detail, detail.Turns[state.selectedTurnIndex], true
}

func showSessionIdentityMetadata(summary query.SessionSummary) bool {
	return summary.Source == usage.SourceCtx && (strings.TrimSpace(summary.ProviderSessionID) != "" || strings.TrimSpace(summary.CtxSessionID) != "")
}

func (state *State) renderWidth() int {
	if state.Width <= 0 {
		return 80
	}
	return state.Width
}

func overviewMetric(label, value string, indent, labelWidth, valueWidth, width int) string {
	label = strings.Repeat("  ", indent) + label
	aligned := "  " + padRightDisplay(label, labelWidth) + " " + padLeftDisplay(value, valueWidth)
	if lipgloss.Width(aligned) <= width {
		return aligned
	}
	return truncate(fmt.Sprintf("  %s: %s", label, value), width)
}

func trendBarWidth(width int) int {
	switch {
	case width >= 80:
		return 18
	case width >= 50:
		return 10
	default:
		return maxInt(3, width-31)
	}
}

func trendCells(width int, row *query.UsageTrend, bar string) []tableCell {
	barWidth := trendBarWidth(width)
	if row == nil {
		return []tableCell{{value: "DATE", width: 8}, {value: "ACTIVITY", width: barWidth}, {value: "TURNS", width: 7, right: true}, {value: "SESSIONS", width: 9, right: true}}
	}
	return []tableCell{{value: row.Date.Format("01-02"), width: 8}, {value: bar, width: barWidth}, {value: formatInt(row.Turns), width: 7, right: true}, {value: formatInt(row.Sessions), width: 9, right: true}}
}

func (state *State) periodInfoLines() []string {
	if state.Filter.From.IsZero() && state.Filter.To.IsZero() {
		return nil
	}
	actual := state.ReadModel.Overview.Period
	if actual.From.IsZero() {
		return []string{"No usage found for the selected period."}
	}
	now := state.now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	requestedFrom, requestedTo := state.Filter.From, state.Filter.To
	if requestedTo.IsZero() {
		requestedTo = now
	} else {
		requestedTo = requestedTo.Add(-time.Nanosecond)
	}
	var messages []string
	if !requestedFrom.IsZero() && dateBefore(requestedFrom, actual.From) {
		messages = append(messages, fmt.Sprintf("selected period starts before the first usage record (%s)", formatDate(actual.From)))
	}
	if !actual.To.IsZero() && dateBefore(actual.To, requestedTo) {
		messages = append(messages, fmt.Sprintf("selected period ends after the last usage record (%s)", formatDate(actual.To)))
	}
	if len(messages) == 0 {
		return nil
	}
	return messages
}

func (state *State) periodNoticeText() string {
	if state.Filter.From.IsZero() && state.Filter.To.IsZero() {
		return ""
	}
	actual := state.ReadModel.Overview.Period
	if actual.From.IsZero() {
		return "No usage found for the selected period."
	}
	if len(state.periodInfoLines()) == 0 {
		return ""
	}
	now := state.now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	requestedTo := state.Filter.To
	if requestedTo.IsZero() {
		requestedTo = now
	} else {
		requestedTo = requestedTo.Add(-time.Nanosecond)
	}
	return fmt.Sprintf("Period partially covered (requested: %s; actual: %s)", formatPeriod(state.Filter.From, requestedTo), formatPeriod(actual.From, actual.To))
}

func (state *State) clearPeriodNotice() {
	state.periodNotice = ""
}

func dateBefore(left, right time.Time) bool {
	left = left.UTC()
	right = right.UTC()
	left = time.Date(left.Year(), left.Month(), left.Day(), 0, 0, 0, 0, time.UTC)
	right = time.Date(right.Year(), right.Month(), right.Day(), 0, 0, 0, 0, time.UTC)
	return left.Before(right)
}

func listHeading(name string, count, selected, width int) string {
	left := name
	right := ""
	if count > 0 {
		right = fmt.Sprintf("%d/%d", selected+1, count)
	}
	return sectionStyle.Render(headingLine(left, right, width))
}

type metadataField struct {
	label string
	value string
}

func metadataLine(indent string, fields ...metadataField) string {
	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, mutedStyle.Render(field.label+":")+" "+safeDisplay(field.value))
	}
	return indent + strings.Join(parts, "   ")
}

func primaryListHeading(name string, count, selected, width int) string {
	left := name
	right := ""
	if count > 0 {
		right = fmt.Sprintf("%d/%d", selected+1, count)
	}
	return titleStyle.Render(headingLine(left, right, width))
}

func headingLine(left, right string, width int) string {
	left = safeDisplay(left)
	right = safeDisplay(right)
	if right == "" {
		return truncate(left, width)
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = truncate(left, maxInt(1, width-lipgloss.Width(right)-1))
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 1 {
		return truncate(left+" "+right, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

type tableCell struct {
	value string
	width int
	right bool
	muted bool
}

func renderTableHeader(width int, cells []tableCell) string {
	return headerStyle.Render(renderTableLine(width, false, cells))
}

func renderTableRow(width int, selected bool, cells []tableCell) string {
	line := renderTableLine(width, selected, cells)
	if selected {
		return selectedStyle.Render(line)
	}
	return line
}

func renderTableLine(width int, selected bool, cells []tableCell) string {
	width = maxInt(1, width)
	cells = fitTableCells(width, cells)
	prefix := "  "
	if selected {
		prefix = "> "
	}
	parts := make([]string, 0, len(cells))
	for _, cell := range cells {
		value := truncate(safeDisplay(cell.value), cell.width)
		if cell.muted && !selected {
			value = mutedStyle.Render(value)
		}
		if cell.right {
			value = padLeftDisplay(value, cell.width)
		} else {
			value = padRightDisplay(value, cell.width)
		}
		parts = append(parts, value)
	}
	line := prefix + strings.Join(parts, "  ")
	if lipgloss.Width(line) > width {
		line = truncate(safeDisplay(line), width)
	}
	if selected {
		line = padRightDisplay(line, width)
	}
	return line
}

func fitTableCells(width int, cells []tableCell) []tableCell {
	result := append([]tableCell(nil), cells...)
	if len(result) == 0 {
		return result
	}
	available := width - 2 - 2*(len(result)-1)
	if available < len(result) {
		available = len(result)
	}
	total := 0
	for _, cell := range result {
		total += maxInt(1, cell.width)
	}
	excess := total - available
	for i := range result {
		if excess <= 0 {
			break
		}
		minimum := 1
		shrink := result[i].width - minimum
		if shrink > excess {
			shrink = excess
		}
		result[i].width -= shrink
		excess -= shrink
	}
	return result
}

func padRightDisplay(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func padLeftDisplay(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return strings.Repeat(" ", padding) + value
}

func modelCells(width int, row *query.ModelSummary) []tableCell {
	wide := width >= 90
	if wide {
		nameWidth := maxInt(8, width-45)
		if row == nil {
			return []tableCell{{value: "MODEL", width: nameWidth}, {value: "SESSIONS", width: 8, right: true}, {value: "TURNS", width: 7, right: true}, {value: "TOKENS", width: 10, right: true}, {value: "LAST USED", width: relativeTimeColumnWidth, right: true}}
		}
		return []tableCell{{value: row.Model.Provider + "/" + row.Model.Name, width: nameWidth}, {value: formatInt(row.Sessions), width: 8, right: true}, {value: formatInt(row.Turns), width: 7, right: true}, {value: formatTokenTotal(row.TokenUsage, row.TokenUsageAvailable), width: 10, right: true}, {value: formatRelativeTime(row.LastUsed), width: relativeTimeColumnWidth, right: true}}
	}
	nameWidth := maxInt(8, width-23)
	if row == nil {
		return []tableCell{{value: "MODEL", width: nameWidth}, {value: "TURNS", width: 7, right: true}, {value: "TOKENS", width: 10, right: true}}
	}
	return []tableCell{{value: row.Model.Provider + "/" + row.Model.Name, width: nameWidth}, {value: formatInt(row.Turns), width: 7, right: true}, {value: formatTokenTotal(row.TokenUsage, row.TokenUsageAvailable), width: 10, right: true}}
}

func modelCellsForRow(width int, row query.ModelSummary) []tableCell {
	return modelCells(width, &row)
}

func skillCells(width int, row *query.SkillSummary) []tableCell {
	if width >= 110 {
		nameWidth := maxInt(8, width-59)
		if row == nil {
			return []tableCell{{value: "SKILL", width: nameWidth}, {value: "USES", width: 7, right: true}, {value: "SESSIONS", width: 8, right: true}, {value: "LAST USED", width: relativeTimeColumnWidth, right: true}, {value: "METHODS", width: 24}}
		}
		return []tableCell{{value: row.Name, width: nameWidth}, {value: formatInt(row.Uses), width: 7, right: true}, {value: formatInt(row.Sessions), width: 8, right: true}, {value: formatRelativeTime(row.LastUsed), width: relativeTimeColumnWidth, right: true}, {value: formatMethodCounts(row.MethodCounts), width: 24}}
	}
	if width >= 70 {
		nameWidth := maxInt(8, width-33)
		if row == nil {
			return []tableCell{{value: "SKILL", width: nameWidth}, {value: "USES", width: 7, right: true}, {value: "SESSIONS", width: 8, right: true}, {value: "LAST USED", width: relativeTimeColumnWidth, right: true}}
		}
		return []tableCell{{value: row.Name, width: nameWidth}, {value: formatInt(row.Uses), width: 7, right: true}, {value: formatInt(row.Sessions), width: 8, right: true}, {value: formatRelativeTime(row.LastUsed), width: relativeTimeColumnWidth, right: true}}
	}
	nameWidth := maxInt(8, width-23)
	if row == nil {
		return []tableCell{{value: "SKILL", width: nameWidth}, {value: "USES", width: 7, right: true}, {value: "SESSIONS", width: 8, right: true}}
	}
	return []tableCell{{value: row.Name, width: nameWidth}, {value: formatInt(row.Uses), width: 7, right: true}, {value: formatInt(row.Sessions), width: 8, right: true}}
}

func skillCellsForRow(width int, row query.SkillSummary) []tableCell {
	return skillCells(width, &row)
}

func sessionName(title, id string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return id
	}
	return title + " (" + id + ")"
}

func sessionDisplayName(title, id string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	return "(" + shortSessionID(id) + ")"
}

func sessionDisplayCell(title, id string, width int) tableCell {
	title = strings.TrimSpace(title)
	return tableCell{value: sessionDisplayName(title, id), width: width, muted: title == ""}
}

func shortSessionID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func sessionIdentity(source usage.SourceKind, agent, id string) string {
	identity := string(source) + "/" + id
	if agent != "" && agent != "unknown" && agent != string(source) {
		identity = string(source) + "/" + agent + "/" + id
	}
	return identity
}

func sessionLabel(title string, source usage.SourceKind, agent, id string, aborted bool) string {
	label := sessionName(title, sessionIdentity(source, agent, id))
	if aborted {
		label = "! " + label
	}
	return label
}

func sessionCells(width int, row *query.SessionSummary) []tableCell {
	if width >= 90 {
		available := maxInt(30, width-8-relativeTimeColumnWidth)
		nameWidth := maxInt(16, available*2/5)
		projectWidth := maxInt(12, available-nameWidth)
		if row == nil {
			return []tableCell{{value: "SESSION", width: nameWidth}, {value: "PROJECT", width: projectWidth}, {value: "LAST USED", width: relativeTimeColumnWidth, right: true}}
		}
		return []tableCell{sessionDisplayCell(row.Title, row.ID, nameWidth), {value: emptyDash(row.ProjectPath), width: projectWidth}, {value: formatRelativeTime(row.EndedAt), width: relativeTimeColumnWidth, right: true}}
	}
	nameWidth := maxInt(8, width-6-relativeTimeColumnWidth)
	if row == nil {
		return []tableCell{{value: "SESSION", width: nameWidth}, {value: "LAST USED", width: relativeTimeColumnWidth, right: true}}
	}
	return []tableCell{sessionDisplayCell(row.Title, row.ID, nameWidth), {value: formatRelativeTime(row.EndedAt), width: relativeTimeColumnWidth, right: true}}
}

func sessionCellsForRow(width int, row query.SessionSummary) []tableCell {
	return sessionCells(width, &row)
}

func modelSessionCells(width int, row *query.ModelSessionUsage) []tableCell {
	if width >= 80 {
		nameWidth := maxInt(16, width-48)
		projectWidth := 18
		if row == nil {
			return []tableCell{{value: "SESSION", width: nameWidth}, {value: "PROJECT", width: projectWidth}, {value: "TURNS", width: 7, right: true}, {value: "TOKENS", width: 10, right: true}}
		}
		return []tableCell{sessionDisplayCell(row.Title, row.ID, nameWidth), {value: emptyDash(row.Project), width: projectWidth}, {value: formatInt(row.Turns), width: 7, right: true}, {value: formatTokenTotal(row.TokenUsage, row.TokenUsage.TotalTokens != 0), width: 10, right: true}}
	}
	nameWidth := maxInt(8, width-23)
	if row == nil {
		return []tableCell{{value: "SESSION", width: nameWidth}, {value: "TURNS", width: 7, right: true}, {value: "TOKENS", width: 10, right: true}}
	}
	return []tableCell{sessionDisplayCell(row.Title, row.ID, nameWidth), {value: formatInt(row.Turns), width: 7, right: true}, {value: formatTokenTotal(row.TokenUsage, row.TokenUsage.TotalTokens != 0), width: 10, right: true}}
}

func modelSessionCellsForRow(width int, row query.ModelSessionUsage) []tableCell {
	return modelSessionCells(width, &row)
}

func skillSessionCells(width int, row *query.SkillSessionUsage) []tableCell {
	if width >= 100 {
		nameWidth := maxInt(16, width-47)
		if row == nil {
			return []tableCell{{value: "SESSION", width: nameWidth}, {value: "TURNS", width: 7, right: true}, {value: "METHODS", width: 24}, {value: "LAST USED", width: relativeTimeColumnWidth, right: true}}
		}
		return []tableCell{sessionDisplayCell(row.Title, row.SessionID, nameWidth), {value: formatInt(row.Turns), width: 7, right: true}, {value: formatMethodCounts(row.MethodCounts), width: 24}, {value: formatRelativeTime(row.LastUsed), width: relativeTimeColumnWidth, right: true}}
	}
	nameWidth := maxInt(8, width-23)
	if row == nil {
		return []tableCell{{value: "SESSION", width: nameWidth}, {value: "TURNS", width: 7, right: true}, {value: "LAST USED", width: relativeTimeColumnWidth, right: true}}
	}
	return []tableCell{sessionDisplayCell(row.Title, row.SessionID, nameWidth), {value: formatInt(row.Turns), width: 7, right: true}, {value: formatRelativeTime(row.LastUsed), width: relativeTimeColumnWidth, right: true}}
}

func skillSessionCellsForRow(width int, row query.SkillSessionUsage) []tableCell {
	return skillSessionCells(width, &row)
}

func turnCells(width int, row *query.TurnSummary) []tableCell {
	return turnCellsWithLayout(width, row, "2006-01-02 15:04")
}

func turnCellsForSession(width int, row *query.TurnSummary, turns []query.TurnSummary) []tableCell {
	return turnCellsWithLayout(width, row, turnTimeLayout(turns))
}

func turnCellsWithLayout(width int, row *query.TurnSummary, timeLayout string) []tableCell {
	values := []string{"TURN", "MODEL", "TOKENS", "TOOLS", "SKILLS", "STATUS", "TIME"}
	if row != nil {
		status := "done"
		if row.Aborted {
			status = "aborted"
		}
		values = []string{
			formatTurnOrdinal(*row),
			emptyDash(turnModelNames(row.Models)),
			formatTokenTotal(row.TokenUsage, row.TokenUsageAvailable),
			formatInt(len(row.Tools)),
			formatInt(len(row.Skills)),
			status,
			formatTurnTime(row.StartedAt, timeLayout),
		}
	}
	timeWidth := len(timeLayout)
	widths := []int{6, 14, 8, 6, 6, 8, timeWidth}
	if width >= 110 {
		widths = []int{8, 28, 10, 7, 7, 8, timeWidth}
	} else if width < 80 {
		widths = []int{5, 12, 7, 5, 5, 7, timeWidth}
	}
	cells := make([]tableCell, len(values))
	for i, value := range values {
		cells[i] = tableCell{value: value, width: widths[i], right: i == 2 || i == 6}
	}
	return cells
}

func turnTimeLayout(turns []query.TurnSummary) string {
	const fullLayout = "2006-01-02 15:04"
	var first, last time.Time
	for _, turn := range turns {
		if turn.StartedAt.IsZero() {
			continue
		}
		started := turn.StartedAt.UTC()
		if first.IsZero() || started.Before(first) {
			first = started
		}
		if last.IsZero() || started.After(last) {
			last = started
		}
	}
	if first.IsZero() || first.Year() != last.Year() {
		return fullLayout
	}
	if first.YearDay() != last.YearDay() {
		return "01/02 15:04"
	}
	return "15:04"
}

func formatTurnTime(value time.Time, layout string) string {
	if value.IsZero() {
		return "—"
	}
	return value.UTC().Format(layout)
}

func formatTurnOrdinal(row query.TurnSummary) string {
	if row.Ordinal > 0 {
		return fmt.Sprintf("%02d", row.Ordinal)
	}
	return truncate(row.ID, 8)
}

func modelNames(models []usage.ModelRef) string {
	values := make([]string, 0, len(models))
	for _, model := range models {
		values = append(values, model.Provider+"/"+model.Name)
	}
	return strings.Join(values, ", ")
}

func turnModelNames(models []usage.ModelRef) string {
	values := make([]string, 0, len(models))
	for _, model := range models {
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = strings.TrimSpace(model.Provider)
		}
		if name == "" {
			name = "unknown"
		}
		values = append(values, name)
	}
	return strings.Join(values, ", ")
}

func turnStatus(turn *query.TurnSummary) string {
	if turn.Aborted {
		return "aborted"
	}
	return "done"
}

func formatTurnItems(items []string) string {
	return emptyDash(strings.Join(items, ", "))
}

func formatInt(value int) string { return formatCompactInt(int64(value)) }

func formatTokenTotal(value usage.TokenUsage, available bool) string {
	if !available {
		return "—"
	}
	return formatCompactInt(value.TotalTokens)
}

func formatCompactInt(value int64) string {
	if value < 0 {
		return "-" + formatCompactInt(-value)
	}
	if value >= 1_000_000_000 {
		return fmt.Sprintf("%.1fB", float64(value)/1_000_000_000)
	}
	if value >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	}
	if value >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	}
	return strconv.FormatInt(value, 10)
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func fitBody(lines []string, height int) []string {
	if height <= 0 || len(lines) <= height {
		return lines
	}
	if height == 1 {
		return []string{"…"}
	}
	result := append([]string(nil), lines[:height-1]...)
	return append(result, "…")
}

func formatPeriod(from, to time.Time) string {
	if from.IsZero() && to.IsZero() {
		return "no data"
	}
	left, right := formatDate(from), formatDate(to)
	if right == "—" {
		return left + " onward"
	}
	if left == "—" {
		return "through " + right
	}
	return left + " to " + right
}

func formatAgents(agents []string, fallback string) string {
	values := append([]string(nil), agents...)
	if len(values) == 0 && strings.TrimSpace(fallback) != "" {
		values = strings.Split(fallback, ",")
	}
	seen := make(map[string]struct{}, len(values))
	names := make([]string, 0, len(values))
	for _, value := range values {
		id := usage.CanonicalAgentID(value)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		names = append(names, usage.AgentDisplayName(id))
	}
	if len(names) == 0 {
		return "—"
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func cloneSourcePaths(values map[usage.SourceKind]string) map[usage.SourceKind]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[usage.SourceKind]string, len(values))
	for source, path := range values {
		result[source] = path
	}
	return result
}

func inputSources(input query.Input) []usage.SourceKind {
	values := make([]usage.SourceKind, 0, len(input.Sources)+1)
	values = append(values, input.Sources...)
	if input.Source != "" {
		values = append(values, input.Source)
	}
	for _, turn := range input.Turns {
		values = append(values, turn.Source.Source)
	}
	for _, session := range input.Sessions {
		values = append(values, session.Source.Source)
	}
	return orderedSources(values)
}

func orderedSources(values []usage.SourceKind) []usage.SourceKind {
	seen := make(map[usage.SourceKind]struct{}, len(values))
	for _, value := range values {
		if value.Valid() {
			seen[value] = struct{}{}
		}
	}
	result := make([]usage.SourceKind, 0, len(seen))
	for _, value := range usage.AllSourceKinds() {
		if _, ok := seen[value]; ok {
			result = append(result, value)
		}
	}
	return result
}

func (state *State) sourceOptions() []usage.SourceKind {
	values := inputSources(state.Input)
	values = append(values, state.Filter.Sources...)
	if state.Filter.Source != "" {
		values = append(values, state.Filter.Source)
	}
	return orderedSources(values)
}

func (state *State) sourceEnabled(source usage.SourceKind) bool {
	if state.Filter.Sources == nil {
		return state.Filter.Source == "" || state.Filter.Source == source
	}
	for _, selected := range state.Filter.Sources {
		if selected == source {
			return true
		}
	}
	return false
}

func formatDate(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.UTC().Format("2006-01-02")
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.UTC().Format("2006-01-02 15:04")
}

func formatRelativeTime(value time.Time) string {
	return formatRelativeTimeAt(value, time.Now().UTC())
}

func formatRelativeTimeAt(value, now time.Time) string {
	if value.IsZero() {
		return "—"
	}
	elapsed := now.Sub(value)
	if elapsed <= 0 || elapsed < time.Minute {
		return "now"
	}
	switch {
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm ago", int(elapsed/time.Minute))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(elapsed/time.Hour))
	case elapsed < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(elapsed/(24*time.Hour)))
	case elapsed < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", maxInt(1, int(elapsed/(30*24*time.Hour))))
	default:
		return fmt.Sprintf("%dy ago", maxInt(1, int(elapsed/(365*24*time.Hour))))
	}
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

type labeledCount struct {
	label string
	value int
}

func formatLabeledCounts(values ...labeledCount) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value.value <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d", value.label, value.value))
	}
	if len(parts) == 0 {
		return "None"
	}
	return strings.Join(parts, ", ")
}

func normalizedCount(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func formatMethodCounts(values map[usage.SkillEvidenceMethod]int) string {
	keys := make([]usage.SkillEvidenceMethod, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", key, values[key]))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ",")
}

func window(count, offset, height int) (int, int) {
	if count == 0 {
		return 0, 0
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= count {
		offset = count - 1
	}
	if height < 1 {
		height = 1
	}
	end := offset + height
	if end > count {
		end = count
	}
	return offset, end
}

func boundLines(lines []string, width int) string {
	if width <= 0 {
		return strings.Join(lines, "\n")
	}
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if lipgloss.Width(line) > width {
			line = ansi.Truncate(line, width, "…")
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func boundView(lines []string, width, height int) string {
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	return boundLines(lines, width)
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = safeDisplay(value)
	runes := []rune(value)
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	result := make([]rune, 0, len(runes))
	for _, value := range runes {
		candidate := string(result) + string(value) + "…"
		if lipgloss.Width(candidate) > width {
			break
		}
		result = append(result, value)
	}
	return string(result) + "…"
}

func safeDisplay(value string) string {
	var builder strings.Builder
	for _, value := range value {
		switch {
		case value == '\x00':
			builder.WriteRune('/')
		case value == '\n' || value == '\r' || value == '\t':
			builder.WriteRune(' ')
		case unicode.IsControl(value) || value == '\u007f':
			// Control characters can corrupt the terminal layout, so omit them.
		default:
			builder.WriteRune(value)
		}
	}
	return builder.String()
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}
