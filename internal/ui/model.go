package ui

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type node struct {
	path, label, value, kind string
	searchText               string
	fieldPrefix, fieldType   string
	summaryLabel             string
	summaryValue             string
	summaryKind              string
	children                 func() []*node
}

type row struct {
	n     *node
	depth int
}

type searchVisit struct {
	node      *node
	ancestors []string
	payload   int
}

type searchHit struct {
	path      string
	ancestors []string
	payload   int
}

type searchStepMsg struct{ generation int }

type Model struct {
	messages                         []protoreflect.Message
	roots                            []*node
	typeName, typePrefix, sourceInfo string
	cursor, offset, width, height    int
	expanded                         map[string]bool
	searchInput, searching           bool
	searchInputText, searchQuery     string
	searchStatus                     string
	searchGeneration, searchPayload  int
	searchStack                      []searchVisit
	searchActive                     bool
}

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1)
	selectedStyle = lipgloss.NewStyle().Bold(true).Background(lipgloss.Color("236"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	typeStyles    = map[string]lipgloss.Style{
		"string":  lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		"bytes":   lipgloss.NewStyle().Foreground(lipgloss.Color("205")),
		"number":  lipgloss.NewStyle().Foreground(lipgloss.Color("45")),
		"bool":    lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
		"enum":    lipgloss.NewStyle().Foreground(lipgloss.Color("141")),
		"message": lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
		"unset":   dimStyle,
	}
)

func New(messages []protoreflect.Message, payloadSizes []int, typeName, typePrefix, sourceInfo string) Model {
	m := Model{messages: messages, typeName: trimTypePrefix(typeName, typePrefix), typePrefix: typePrefix, sourceInfo: sourceInfo, expanded: make(map[string]bool)}
	m.roots = make([]*node, len(messages))
	payloadNumberWidth := len(strconv.Itoa(max(1, len(messages))))
	maxPayloadSize := 0
	for _, size := range payloadSizes {
		maxPayloadSize = max(maxPayloadSize, size)
	}
	payloadSizeWidth := len(strconv.Itoa(maxPayloadSize))
	for i, message := range messages {
		messageType := trimTypePrefix(string(message.Descriptor().FullName()), typePrefix)
		size := 0
		if i < len(payloadSizes) {
			size = payloadSizes[i]
		}
		root := messageNode(payloadPath(i), fmt.Sprintf("payload %*d · %*d bytes", payloadNumberWidth, i+1, payloadSizeWidth, size), message, typePrefix)
		root.value = messageType
		root.searchText += fmt.Sprintf(" payload %d", i+1)
		m.roots[i] = root
	}
	if len(messages) > 0 {
		m.expanded[payloadPath(0)] = true
	}
	return m
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case searchStepMsg:
		if !m.searching || msg.generation != m.searchGeneration {
			return m, nil
		}
		hit, done := m.advanceSearch(1500)
		if hit != nil {
			m.searching = false
			m.selectSearchHit(*hit)
			m.searchStatus = fmt.Sprintf("Match in payload %d/%d · n find next · / new search", hit.payload+1, len(m.messages))
			return m, nil
		}
		if done {
			m.searching = false
			m.searchStatus = fmt.Sprintf("No more matches for %q · n search from start · / new search", m.searchQuery)
			return m, nil
		}
		return m, searchStepCmd(m.searchGeneration)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if len(m.messages) > 0 {
			rowCount := len(m.rows())
			m.clampCursor(rowCount)
			m.ensureCursorVisible(rowCount)
		}
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.searchInput {
			switch msg.String() {
			case "esc":
				m.searchInput = false
			case "enter":
				query := strings.TrimSpace(m.searchInputText)
				if query != "" {
					m.searchInput = false
					m.beginSearch(query)
					return m, searchStepCmd(m.searchGeneration)
				}
			case "backspace":
				if len(m.searchInputText) > 0 {
					_, size := utf8.DecodeLastRuneInString(m.searchInputText)
					m.searchInputText = m.searchInputText[:len(m.searchInputText)-size]
				}
			default:
				if msg.Type == tea.KeyRunes {
					m.searchInputText += string(msg.Runes)
				}
			}
			return m, nil
		}
		if m.searching {
			if msg.String() == "esc" {
				m.searching = false
				m.searchGeneration++
				m.searchStatus = "Search cancelled"
			} else if msg.String() == "q" {
				return m, tea.Quit
			}
			return m, nil
		}
		rows := m.rows()
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "/":
			m.searchInput = true
			m.searchInputText = ""
			m.searchStatus = ""
			return m, nil
		case "up", "k":
			m.cursor--
		case "down", "j":
			m.cursor++
		case "home", "g":
			m.cursor = 0
		case "end", "G":
			m.cursor = len(rows) - 1
		case "right", "l", "enter", " ":
			if len(rows) > 0 && rows[m.clampCursor(len(rows))].n.children != nil {
				m.expanded[rows[m.clampCursor(len(rows))].n.path] = true
			}
		case "left", "h":
			if len(rows) > 0 {
				r := rows[m.clampCursor(len(rows))]
				if m.expanded[r.n.path] {
					delete(m.expanded, r.n.path)
				} else if at := strings.LastIndex(r.n.path, "/"); at > 0 {
					parent := r.n.path[:at]
					delete(m.expanded, parent)
					for i, rr := range rows {
						if rr.n.path == parent {
							m.cursor = i
							break
						}
					}
				}
			}
		case "n":
			if m.searchQuery != "" {
				if m.searchPayload >= len(m.messages) && len(m.searchStack) == 0 {
					m.beginSearch(m.searchQuery)
				} else {
					m.searching = true
					m.searchGeneration++
					m.searchStatus = ""
				}
				return m, searchStepCmd(m.searchGeneration)
			}
		case "pgdown":
			m.cursor += max(1, m.bodyHeight())
		case "pgup":
			m.cursor -= max(1, m.bodyHeight())
		}
		rowCount := len(m.rows())
		m.clampCursor(rowCount)
		m.ensureCursorVisible(rowCount)
	}
	return m, nil
}

func searchStepCmd(generation int) tea.Cmd {
	return tea.Tick(time.Millisecond, func(time.Time) tea.Msg { return searchStepMsg{generation} })
}

func (m *Model) clampCursor(n int) int {
	if n <= 0 {
		m.cursor = 0
		return 0
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	return m.cursor
}

func (m *Model) ensureCursorVisible(rowCount int) {
	bodyHeight := m.bodyHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	} else if m.cursor >= m.offset+bodyHeight {
		m.offset = m.cursor - bodyHeight + 1
	}
	maxOffset := max(0, rowCount-bodyHeight)
	m.offset = min(max(0, m.offset), maxOffset)
}

func (m *Model) beginSearch(query string) {
	m.searchQuery = query
	m.searching = true
	m.searchStatus = ""
	m.searchPayload = 0
	m.searchStack = nil
	m.searchActive = false
	m.searchGeneration++
}

func (m *Model) advanceSearch(limit int) (*searchHit, bool) {
	query := strings.ToLower(m.searchQuery)
	for visited := 0; visited < limit; visited++ {
		if len(m.searchStack) == 0 {
			if m.searchActive {
				m.searchPayload++
				m.searchActive = false
			}
			if m.searchPayload >= len(m.messages) {
				return nil, true
			}
			m.searchStack = append(m.searchStack, searchVisit{node: m.roots[m.searchPayload], payload: m.searchPayload})
			m.searchActive = true
		}

		last := len(m.searchStack) - 1
		visit := m.searchStack[last]
		m.searchStack = m.searchStack[:last]
		if visit.node.children != nil {
			children := visit.node.children()
			ancestors := append(append([]string(nil), visit.ancestors...), visit.node.path)
			for i := len(children) - 1; i >= 0; i-- {
				m.searchStack = append(m.searchStack, searchVisit{node: children[i], ancestors: ancestors, payload: visit.payload})
			}
		}
		if strings.Contains(strings.ToLower(visit.node.searchText), query) {
			return &searchHit{path: visit.node.path, ancestors: visit.ancestors, payload: visit.payload}, false
		}
	}
	return nil, false
}

func (m *Model) selectSearchHit(hit searchHit) {
	m.expanded = make(map[string]bool)
	for _, path := range hit.ancestors {
		m.expanded[path] = true
	}
	m.expanded[hit.path] = true
	m.cursor, m.offset = 0, 0
	rows := m.rows()
	for i, row := range rows {
		if row.n.path == hit.path {
			m.cursor = i
			break
		}
	}
	m.ensureCursorVisible(len(rows))
}

func (m Model) bodyHeight() int { return max(1, m.height-4) }

func (m Model) View() string {
	if len(m.messages) == 0 {
		return "No messages\n"
	}
	width := max(20, m.width)
	title := fmt.Sprintf(" protoview  %s  %d payloads ", m.typeName, len(m.messages))
	var b strings.Builder
	b.WriteString(headerStyle.Width(width).Render(title))
	b.WriteByte('\n')
	b.WriteString(dimStyle.Render(m.sourceInfo))
	b.WriteByte('\n')
	rows := m.rows()
	cursor := m.cursor
	bodyHeight := m.bodyHeight()
	offset := min(max(0, m.offset), max(0, len(rows)-bodyHeight))
	end := min(len(rows), offset+bodyHeight)
	for i := offset; i < end; i++ {
		r := rows[i]
		marker := "  "
		if r.n.children != nil {
			if m.expanded[r.n.path] {
				marker = "▾ "
			} else {
				marker = "▸ "
			}
		}
		line := strings.Repeat("  ", r.depth) + marker + r.n.label
		if r.n.value != "" {
			line += dimStyle.Render(" = ") + typeStyles[r.n.kind].Render(r.n.value)
		}
		if r.n.summaryValue != "" {
			line += dimStyle.Render(" → "+r.n.summaryLabel+" = ") + typeStyles[r.n.summaryKind].Render(r.n.summaryValue)
		}
		line = truncate(line, width)
		if i == cursor {
			line = selectedStyle.Width(width).Render(line)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for i := end - offset; i < bodyHeight; i++ {
		b.WriteByte('\n')
	}
	switch {
	case m.searchInput:
		b.WriteString(" Search: " + m.searchInputText + "█" + dimStyle.Render("  Enter find · Esc cancel"))
	case m.searching:
		b.WriteString(m.searchProgress(width))
	case m.searchStatus != "":
		b.WriteString(dimStyle.Render(" " + m.searchStatus))
	default:
		b.WriteString(dimStyle.Render(" ↑/↓ move  → expand  ← collapse  / search  n next  g/G ends  q quit"))
	}
	return b.String()
}

func (m Model) searchProgress(width int) string {
	progress := float64(m.searchPayload) / float64(max(1, len(m.messages)))
	barWidth := min(32, max(8, width-38))
	filled := min(barWidth, int(progress*float64(barWidth)))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return fmt.Sprintf(" Search [%s] %3.0f%%  %q  Esc cancel", bar, progress*100, m.searchQuery)
}

func (m Model) rows() []row {
	var out []row
	var walk func(*node, int)
	walk = func(n *node, depth int) {
		out = append(out, row{n, depth})
		if n.children != nil && m.expanded[n.path] {
			for _, child := range n.children() {
				walk(child, depth+1)
			}
		}
	}
	for _, root := range m.roots {
		walk(root, 0)
	}
	return out
}

func payloadPath(index int) string { return fmt.Sprintf("payload/%d", index) }

func messageNode(path, label string, msg protoreflect.Message, typePrefix string) *node {
	fullName := string(msg.Descriptor().FullName())
	node := &node{path: path, label: label, kind: "message", searchText: fullName + " " + label, children: func() []*node {
		fields := msg.Descriptor().Fields()
		result := make([]*node, 0, fields.Len()+1)
		setPerOneof := make(map[protoreflect.FullName]int)
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			if oneof := fd.ContainingOneof(); oneof != nil && msg.Has(fd) {
				setPerOneof[oneof.FullName()]++
			}
		}
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			if oneof := fd.ContainingOneof(); oneof != nil && setPerOneof[oneof.FullName()] == 1 && !msg.Has(fd) {
				continue
			}
			result = append(result, fieldNode(path+"/"+string(fd.Name()), fd, msg, typePrefix))
		}
		if unknown := msg.GetUnknown(); len(unknown) > 0 {
			result = append(result, &node{path: path + "/$unknown", label: "unknown fields", value: fmt.Sprintf("%d bytes", len(unknown)), kind: "bytes", searchText: fmt.Sprintf("unknown fields %d bytes", len(unknown))})
		}
		alignFieldLabels(result)
		return result
	}}
	if summaryLabel, summaryValue, summaryKind, ok := primitiveSummary(msg); ok {
		node.summaryLabel = summaryLabel
		node.summaryValue = summaryValue
		node.summaryKind = summaryKind
	}
	return node
}

func primitiveSummary(msg protoreflect.Message) (string, string, string, bool) {
	if len(msg.GetUnknown()) > 0 {
		return "", "", "", false
	}
	fields := msg.Descriptor().Fields()
	setPerOneof := make(map[protoreflect.FullName]int)
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if oneof := fd.ContainingOneof(); oneof != nil && msg.Has(fd) {
			setPerOneof[oneof.FullName()]++
		}
	}
	var candidate protoreflect.FieldDescriptor
	visible := 0
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if oneof := fd.ContainingOneof(); oneof != nil && setPerOneof[oneof.FullName()] == 1 && !msg.Has(fd) {
			continue
		}
		visible++
		candidate = fd
	}
	if visible != 1 || !msg.Has(candidate) || candidate.IsList() || candidate.IsMap() || candidate.Message() != nil {
		return "", "", "", false
	}
	value, kind := scalarValue(candidate, msg.Get(candidate))
	return string(candidate.Name()), value, kind, true
}

func alignFieldLabels(nodes []*node) {
	maxPrefixWidth := 0
	for _, node := range nodes {
		if node.fieldType != "" {
			maxPrefixWidth = max(maxPrefixWidth, lipgloss.Width(node.fieldPrefix))
		}
	}
	for _, node := range nodes {
		if node.fieldType != "" {
			node.label = node.fieldPrefix + strings.Repeat(" ", maxPrefixWidth-lipgloss.Width(node.fieldPrefix)) + " · " + node.fieldType
		}
	}

	maxWidth := 0
	for _, node := range nodes {
		maxWidth = max(maxWidth, lipgloss.Width(node.label))
	}
	for _, node := range nodes {
		node.label += strings.Repeat(" ", maxWidth-lipgloss.Width(node.label))
	}
}

func fieldNode(path string, fd protoreflect.FieldDescriptor, msg protoreflect.Message, typePrefix string) *node {
	prefix := string(fd.Name())
	typeName := styledFieldType(fd, typePrefix)
	decorate := func(node *node) *node {
		node.fieldPrefix = prefix
		node.fieldType = typeName
		node.label = prefix + " · " + typeName
		node.searchText += " " + prefix + " " + plainFieldType(fd) + " " + node.value
		return node
	}
	v := msg.Get(fd)
	if fd.IsList() {
		list := v.List()
		return decorate(&node{path: path, value: fmt.Sprintf("%d items", list.Len()), kind: "message", children: func() []*node {
			out := make([]*node, list.Len())
			for i := 0; i < list.Len(); i++ {
				out[i] = valueNode(fmt.Sprintf("%s/%d", path, i), fmt.Sprintf("[%d]", i), fd, list.Get(i), true, typePrefix)
			}
			return out
		}})
	}
	if fd.IsMap() {
		mp := v.Map()
		return decorate(&node{path: path, value: fmt.Sprintf("%d entries", mp.Len()), kind: "message", children: func() []*node {
			type entry struct {
				key   protoreflect.MapKey
				value protoreflect.Value
			}
			entries := make([]entry, 0, mp.Len())
			mp.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
				entries = append(entries, entry{k, v})
				return true
			})
			sort.Slice(entries, func(i, j int) bool {
				return fmt.Sprint(entries[i].key.Interface()) < fmt.Sprint(entries[j].key.Interface())
			})
			out := make([]*node, 0, len(entries))
			for _, entry := range entries {
				key := fmt.Sprint(entry.key.Interface())
				label := key
				if fd.MapKey().Kind() == protoreflect.StringKind {
					label = strconv.Quote(key)
				}
				node := valueNode(path+"/"+url.PathEscape(key), label, fd.MapValue(), entry.value, true, typePrefix)
				node.searchText += " " + key
				out = append(out, node)
			}
			return out
		}})
	}
	if !msg.Has(fd) {
		return decorate(&node{path: path, value: "<unset>", kind: "unset"})
	}
	return decorate(valueNode(path, prefix, fd, v, false, typePrefix))
}

func valueNode(path, label string, fd protoreflect.FieldDescriptor, v protoreflect.Value, showMessageType bool, typePrefix string) *node {
	if fd.Message() != nil {
		node := messageNode(path, label, v.Message(), typePrefix)
		if showMessageType {
			node.value = trimTypePrefix(string(v.Message().Descriptor().FullName()), typePrefix)
		}
		return node
	}
	value, kind := scalarValue(fd, v)
	return &node{path: path, label: label, value: value, kind: kind, searchText: label + " " + rawScalarSearchValue(fd, v)}
}

func scalarValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) (string, string) {
	kind, value := "number", fmt.Sprint(v.Interface())
	switch fd.Kind() {
	case protoreflect.StringKind:
		kind, value = "string", strconv.Quote(v.String())
	case protoreflect.BytesKind:
		kind = "bytes"
		data := v.Bytes()
		shown := data
		if len(shown) > 32 {
			shown = shown[:32]
		}
		value = "0x" + hex.EncodeToString(shown)
		if len(shown) < len(data) {
			value += fmt.Sprintf("… (%d bytes)", len(data))
		}
	case protoreflect.BoolKind:
		kind = "bool"
	case protoreflect.EnumKind:
		kind = "enum"
		ev := fd.Enum().Values().ByNumber(v.Enum())
		if ev != nil {
			value = fmt.Sprintf("%s (%d)", ev.Name(), v.Enum())
		}
	}
	return value, kind
}

func rawScalarSearchValue(fd protoreflect.FieldDescriptor, v protoreflect.Value) string {
	if fd.Kind() == protoreflect.BytesKind {
		return hex.EncodeToString(v.Bytes())
	}
	if fd.Kind() == protoreflect.EnumKind {
		if value := fd.Enum().Values().ByNumber(v.Enum()); value != nil {
			return fmt.Sprintf("%s %d", value.Name(), v.Enum())
		}
	}
	return fmt.Sprint(v.Interface())
}

func plainFieldType(fd protoreflect.FieldDescriptor) string {
	if fd.IsMap() {
		return "map<" + plainSingleType(fd.MapKey()) + ", " + plainSingleType(fd.MapValue()) + ">"
	}
	return plainSingleType(fd)
}

func plainSingleType(fd protoreflect.FieldDescriptor) string {
	if fd.Message() != nil {
		return string(fd.Message().FullName())
	}
	if fd.Enum() != nil {
		return string(fd.Enum().FullName())
	}
	return fd.Kind().String()
}

func styledFieldType(fd protoreflect.FieldDescriptor, typePrefix string) string {
	if fd.IsMap() {
		return dimStyle.Render("map<") + styledSingleType(fd.MapKey(), typePrefix) + dimStyle.Render(", ") + styledSingleType(fd.MapValue(), typePrefix) + dimStyle.Render(">")
	}
	if fd.IsList() {
		return styledSingleType(fd, typePrefix)
	}
	return styledSingleType(fd, typePrefix)
}

func styledSingleType(fd protoreflect.FieldDescriptor, typePrefix string) string {
	name, kind := fd.Kind().String(), "number"
	if fd.Message() != nil {
		name, kind = trimTypePrefix(string(fd.Message().FullName()), typePrefix), "message"
	} else if fd.Enum() != nil {
		name, kind = trimTypePrefix(string(fd.Enum().FullName()), typePrefix), "enum"
	} else {
		switch fd.Kind() {
		case protoreflect.StringKind:
			kind = "string"
		case protoreflect.BytesKind:
			kind = "bytes"
		case protoreflect.BoolKind:
			kind = "bool"
		}
	}
	return typeStyles[kind].Render(name)
}

func trimTypePrefix(name, prefix string) string {
	if prefix == "" {
		return name
	}
	return strings.TrimPrefix(name, prefix+".")
}

func truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}
