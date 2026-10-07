package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestOneofShowsOnlySelectedField(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("choice.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name:      proto.String("Choice"),
			OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("value")}},
			Field: []*descriptorpb.FieldDescriptorProto{
				{Name: proto.String("number"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(), OneofIndex: proto.Int32(0)},
				{Name: proto.String("text"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), OneofIndex: proto.Int32(0)},
			},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := file.Messages().Get(0)
	message := dynamicpb.NewMessage(descriptor)
	message.Set(descriptor.Fields().ByName("text"), protoreflectValueOfString("selected"))

	node := messageNode("root", "Choice", message, "test")
	children := node.children()
	if len(children) != 1 || !strings.Contains(children[0].label, "text") {
		t.Fatalf("expected only selected oneof field, got %#v", children)
	}
	if node.summaryLabel != "text" || node.summaryValue != `"selected"` || node.summaryKind != "string" {
		t.Fatalf("unexpected oneof summary: %s = %s (%s)", node.summaryLabel, node.summaryValue, node.summaryKind)
	}
}

func TestStringMapKeyEscapesTerminalControls(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("map.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Record"),
			NestedType: []*descriptorpb.DescriptorProto{{
				Name:    proto.String("LabelsEntry"),
				Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("key"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("value"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			}},
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name: proto.String("labels"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(), TypeName: proto.String(".test.Record.LabelsEntry"),
			}},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := file.Messages().Get(0)
	message := dynamicpb.NewMessage(descriptor)
	field := descriptor.Fields().ByName("labels")
	emptyMap := fieldNode("root/labels", field, message, "test")
	if emptyMap.value != "0 entries" || emptyMap.kind != "message" || len(emptyMap.children()) != 0 {
		t.Fatalf("empty map node = %+v", emptyMap)
	}
	rawKey := "\x1b]0;injected\a\nkey"
	message.Mutable(field).Map().Set(protoreflect.ValueOfString(rawKey).MapKey(), protoreflect.ValueOfString("value"))

	mapNode := messageNode("root", "Record", message, "test").children()[0]
	entry := mapNode.children()[0]
	if entry.label != strconv.Quote(rawKey) {
		t.Fatalf("map key label = %q, want %q", entry.label, strconv.Quote(rawKey))
	}
	if strings.ContainsAny(entry.label, "\x1b\a\n") {
		t.Fatalf("map key label contains terminal controls: %q", entry.label)
	}
	if entry.path != "root/labels/%1B%5D0%3Binjected%07%0Akey" {
		t.Fatalf("map key path was not escaped: %q", entry.path)
	}
	if !strings.Contains(entry.searchText, rawKey) {
		t.Fatalf("map key search text did not retain raw key: %q", entry.searchText)
	}

	model := New([]protoreflect.Message{message}, []int{42}, "test.Record", "test", "fixture")
	model.width, model.height = 120, 10
	model.expanded["payload/0/labels"] = true
	view := model.View()
	if strings.Contains(view, rawKey) {
		t.Fatalf("rendered view contains raw map key controls: %q", view)
	}
	if !strings.Contains(view, strconv.Quote(rawKey)) {
		t.Fatalf("rendered view does not contain escaped map key: %q", view)
	}
}

func TestMapKeyPathsDoNotCollideWithDescendants(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("map_paths.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Child"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name: proto.String("b"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				}},
			},
			{
				Name: proto.String("Record"),
				NestedType: []*descriptorpb.DescriptorProto{{
					Name:    proto.String("ChildrenEntry"),
					Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: proto.String("key"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
						{Name: proto.String("value"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(), TypeName: proto.String(".test.Child")},
					},
				}},
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name: proto.String("children"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(), TypeName: proto.String(".test.Record.ChildrenEntry"),
				}},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	record := dynamicpb.NewMessage(file.Messages().ByName("Record"))
	children := record.Mutable(record.Descriptor().Fields().ByName("children")).Map()
	for _, key := range []string{"a", "a/b"} {
		child := dynamicpb.NewMessage(file.Messages().ByName("Child"))
		child.Set(child.Descriptor().Fields().ByName("b"), protoreflect.ValueOfString(key))
		children.Set(protoreflect.ValueOfString(key).MapKey(), protoreflect.ValueOfMessage(child))
	}

	entries := messageNode("root", "Record", record, "test").children()[0].children()
	if len(entries) != 2 {
		t.Fatalf("map entries = %d, want 2", len(entries))
	}
	nestedPath := entries[0].children()[0].path
	if entries[0].label != `"a"` || entries[1].label != `"a/b"` {
		t.Fatalf("map entry order = %q, %q", entries[0].label, entries[1].label)
	}
	if nestedPath == entries[1].path {
		t.Fatalf("nested field and map entry share path %q", nestedPath)
	}
	if nestedPath != "root/children/a/b" || entries[1].path != "root/children/a%2Fb" {
		t.Fatalf("paths = nested %q, map entry %q", nestedPath, entries[1].path)
	}
}

func TestViewportScrollsOnlyAtBoundary(t *testing.T) {
	m := Model{height: 8} // Four visible body rows.
	for _, step := range []struct {
		cursor, wantOffset int
	}{
		{3, 0}, // Cursor moves within the initial viewport.
		{4, 1}, // Crossing the bottom scrolls one row.
		{3, 1}, // Moving within the shifted viewport keeps it fixed.
		{1, 1},
		{0, 0}, // Crossing the top scrolls back one row.
	} {
		m.cursor = step.cursor
		m.ensureCursorVisible(10)
		if m.offset != step.wantOffset {
			t.Fatalf("cursor %d: offset = %d, want %d", step.cursor, m.offset, step.wantOffset)
		}
	}
}

func TestUpdateNavigationAndSearchInput(t *testing.T) {
	model := interactionModel()
	if model.Init() != nil {
		t.Fatal("Init() returned a command")
	}

	model, _ = updateModel(t, model, tea.WindowSizeMsg{Width: 72, Height: 8})
	if model.width != 72 || model.height != 8 {
		t.Fatalf("window size = %dx%d", model.width, model.height)
	}

	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("child")})
	if !model.searchInput || model.searchInputText != "child" {
		t.Fatalf("search input = %q, active=%v", model.searchInputText, model.searchInput)
	}
	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyBackspace})
	if model.searchInputText != "chil" {
		t.Fatalf("search input after backspace = %q", model.searchInputText)
	}
	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.searchInput {
		t.Fatal("escape did not close search input")
	}

	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model.searchInputText = "child"
	model, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.searching || command == nil {
		t.Fatalf("enter did not begin search: %+v, %v", model, command)
	}
	message := command()
	model, _ = updateModel(t, model, message)
	if model.searching || !strings.Contains(model.searchStatus, "Match in payload 1/1") {
		t.Fatalf("completed search = searching %v, status %q", model.searching, model.searchStatus)
	}

	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model.searchInputText = "   "
	model, command = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.searchInput || command != nil {
		t.Fatalf("blank search submitted: active=%v command=%v", model.searchInput, command)
	}
}

func TestUpdateSearchStates(t *testing.T) {
	model := interactionModel()
	model.beginSearch("missing")
	model, command := updateModel(t, model, searchStepMsg{generation: model.searchGeneration})
	if model.searching || command != nil || !strings.Contains(model.searchStatus, "No more matches") {
		t.Fatalf("missing search = searching %v, status %q, command %v", model.searching, model.searchStatus, command)
	}

	model.beginSearch("child")
	generation := model.searchGeneration
	model, command = updateModel(t, model, searchStepMsg{generation: generation - 1})
	if !model.searching || command != nil {
		t.Fatalf("stale search step changed search: %+v, %v", model, command)
	}
	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.searching || model.searchGeneration == generation || model.searchStatus != "Search cancelled" {
		t.Fatalf("cancelled search = %+v", model)
	}

	model.searching = true
	if _, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); command == nil {
		t.Fatal("q did not quit during search")
	}
	model.searching = true
	model, command = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if !model.searching || command != nil {
		t.Fatalf("ordinary key changed active search: %+v, %v", model, command)
	}

	children := make([]*node, 1600)
	for i := range children {
		children[i] = &node{path: "payload/0/item/" + strconv.Itoa(i), label: "item"}
	}
	model = interactionModel()
	model.roots[0].children = func() []*node { return children }
	model.beginSearch("missing")
	model, command = updateModel(t, model, searchStepMsg{generation: model.searchGeneration})
	if !model.searching || command == nil {
		t.Fatalf("incremental search did not schedule another step: %+v, %v", model, command)
	}
}

func TestUpdateTreeNavigation(t *testing.T) {
	model := interactionModel()
	keys := []tea.KeyMsg{
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
		{Type: tea.KeyEnd},
		{Type: tea.KeyHome},
		{Type: tea.KeyPgDown},
		{Type: tea.KeyPgUp},
	}
	for _, key := range keys {
		model, _ = updateModel(t, model, key)
	}

	delete(model.expanded, "payload/0")
	model.cursor = 0
	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	if !model.expanded["payload/0"] {
		t.Fatal("right did not expand root")
	}
	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyLeft})
	if model.expanded["payload/0"] {
		t.Fatal("left did not collapse root")
	}

	model.expanded["payload/0"] = true
	model.cursor = 1
	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyLeft})
	if model.expanded["payload/0"] || model.cursor != 0 {
		t.Fatalf("left did not collapse parent: expanded=%v cursor=%d", model.expanded, model.cursor)
	}

	model.searchQuery = "child"
	model.searchPayload = len(model.messages)
	model.searchStack = nil
	model, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if !model.searching || command == nil {
		t.Fatalf("n did not restart search: %+v, %v", model, command)
	}
	model.searching = false
	model.searchPayload = 0
	model.searchStack = []searchVisit{{node: model.roots[0]}}
	model, command = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if !model.searching || command == nil {
		t.Fatalf("n did not resume search: %+v, %v", model, command)
	}

	model.searching = false
	if _, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyCtrlC}); command == nil {
		t.Fatal("ctrl+c did not quit")
	}
	if _, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); command == nil {
		t.Fatal("q did not quit")
	}

	empty := Model{expanded: make(map[string]bool)}
	empty, _ = updateModel(t, empty, tea.WindowSizeMsg{Width: 40, Height: 5})
	if empty.View() != "No messages\n" {
		t.Fatalf("empty View() = %q", empty.View())
	}
	if empty.clampCursor(0) != 0 {
		t.Fatal("empty cursor was not clamped")
	}
	empty.cursor = -1
	if empty.clampCursor(2) != 0 {
		t.Fatal("negative cursor was not clamped")
	}
	empty.cursor = 5
	if empty.clampCursor(2) != 1 {
		t.Fatal("high cursor was not clamped")
	}
}

func TestViewSearchStatuses(t *testing.T) {
	for _, configure := range []func(*Model){
		func(model *Model) { model.searchInput = true; model.searchInputText = "needle" },
		func(model *Model) { model.searching = true; model.searchQuery = "needle" },
		func(model *Model) { model.searchStatus = "Found a match" },
	} {
		model := interactionModel()
		model.width, model.height = 80, 8
		configure(&model)
		if view := model.View(); view == "" {
			t.Fatal("View() returned no output")
		}
	}
}

func interactionModel() Model {
	child := &node{path: "payload/0/child", label: "child", searchText: "child value"}
	root := &node{
		path: "payload/0", label: "payload 1", searchText: "root",
		children: func() []*node { return []*node{child} },
	}
	return Model{
		messages: []protoreflect.Message{nil}, roots: []*node{root},
		typeName: "Record", sourceInfo: "fixture", width: 60, height: 8,
		expanded: map[string]bool{"payload/0": true},
	}
}

func updateModel(t *testing.T, model Model, message tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, command := model.Update(message)
	result, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want ui.Model", updated)
	}
	return result, command
}

func TestTruncateClosesStyledSpan(t *testing.T) {
	input := "value = \x1b[31mabcdefghij\x1b[0m"
	want := "value = \x1b[31mabcde…\x1b[0m"
	if got := truncate(input, 14); got != want {
		t.Fatalf("truncate() = %q, want %q", got, want)
	}
}

func TestTruncateRespectsWideUnicodeWidth(t *testing.T) {
	got := truncate("界界界", 5)
	if got != "界界…" {
		t.Fatalf("truncate() = %q, want %q", got, "界界…")
	}
	if width := lipgloss.Width(got); width != 5 {
		t.Fatalf("truncated width = %d, want 5", width)
	}
}

func TestFieldRenderingAcrossProtobufKinds(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("kinds.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Status"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("UNKNOWN"), Number: proto.Int32(0)},
				{Name: proto.String("READY"), Number: proto.Int32(1)},
			},
		}},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Child"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name: proto.String("name"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				}},
			},
			{
				Name: proto.String("Record"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("text"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("blob"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum()},
					{Name: proto.String("active"), Number: proto.Int32(3), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_BOOL.Enum()},
					{Name: proto.String("status"), Number: proto.Int32(4), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(), TypeName: proto.String(".test.Status")},
					{Name: proto.String("count"), Number: proto.Int32(5), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum()},
					{Name: proto.String("child"), Number: proto.Int32(6), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(), TypeName: proto.String(".test.Child")},
					{Name: proto.String("tags"), Number: proto.Int32(7), Label: descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := file.Messages().ByName("Record")
	message := dynamicpb.NewMessage(descriptor)
	fields := descriptor.Fields()
	message.Set(fields.ByName("text"), protoreflect.ValueOfString("hello"))
	message.Set(fields.ByName("blob"), protoreflect.ValueOfBytes(bytesOfLength(40)))
	message.Set(fields.ByName("active"), protoreflect.ValueOfBool(true))
	message.Set(fields.ByName("status"), protoreflect.ValueOfEnum(1))
	message.Set(fields.ByName("count"), protoreflect.ValueOfInt32(42))
	child := dynamicpb.NewMessage(file.Messages().ByName("Child"))
	child.Set(child.Descriptor().Fields().ByName("name"), protoreflect.ValueOfString("nested"))
	message.Set(fields.ByName("child"), protoreflect.ValueOfMessage(child))
	message.Mutable(fields.ByName("tags")).List().Append(protoreflect.ValueOfString("one"))

	nodes := messageNode("root", "Record", message, "test").children()
	if len(nodes) != fields.Len() {
		t.Fatalf("rendered %d fields, want %d", len(nodes), fields.Len())
	}
	byName := make(map[string]*node, len(nodes))
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		byName[string(field.Name())] = nodes[i]
	}
	if byName["blob"].kind != "bytes" || !strings.Contains(byName["blob"].value, "40 bytes") {
		t.Fatalf("blob node = %+v", byName["blob"])
	}
	if byName["active"].kind != "bool" || byName["active"].value != "true" {
		t.Fatalf("bool node = %+v", byName["active"])
	}
	if byName["status"].kind != "enum" || byName["status"].value != "READY (1)" {
		t.Fatalf("enum node = %+v", byName["status"])
	}
	if byName["tags"].children == nil || len(byName["tags"].children()) != 1 {
		t.Fatalf("list node = %+v", byName["tags"])
	}
	if byName["child"].children == nil || len(byName["child"].children()) != 1 {
		t.Fatalf("message node = %+v", byName["child"])
	}

	shownMessage := valueNode("root/child", "child", fields.ByName("child"), protoreflect.ValueOfMessage(child), true, "test")
	if shownMessage.value != "Child" {
		t.Fatalf("shown message type = %q", shownMessage.value)
	}
	unknownEnum, kind := scalarValue(fields.ByName("status"), protoreflect.ValueOfEnum(99))
	if unknownEnum != "99" || kind != "enum" {
		t.Fatalf("unknown enum = %q (%s)", unknownEnum, kind)
	}
	if got := rawScalarSearchValue(fields.ByName("blob"), protoreflect.ValueOfBytes([]byte{0xab})); got != "ab" {
		t.Fatalf("raw bytes search value = %q", got)
	}
	if got := rawScalarSearchValue(fields.ByName("status"), protoreflect.ValueOfEnum(1)); got != "READY 1" {
		t.Fatalf("raw enum search value = %q", got)
	}
	if got := trimTypePrefix("test.Record", ""); got != "test.Record" {
		t.Fatalf("trimTypePrefix without prefix = %q", got)
	}

	empty := dynamicpb.NewMessage(descriptor)
	if node := fieldNode("root/text", fields.ByName("text"), empty, "test"); node.kind != "unset" {
		t.Fatalf("unset field node = %+v", node)
	}
	if node := fieldNode("root/tags", fields.ByName("tags"), empty, "test"); node.value != "0 items" || node.kind != "message" || len(node.children()) != 0 {
		t.Fatalf("empty list node = %+v", node)
	}
}

func bytesOfLength(length int) []byte {
	result := make([]byte, length)
	for i := range result {
		result[i] = byte(i)
	}
	return result
}

func TestSearchFindsValuesAndExpandsTheirMessage(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("search.proto"),
		Package: proto.String("test"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Record"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{Name: proto.String("description"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				{Name: proto.String("catalog"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
			},
		}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := file.Messages().Get(0)
	message := dynamicpb.NewMessage(descriptor)
	message.Set(descriptor.Fields().Get(0), protoreflect.ValueOfString("Orion Nebula"))
	message.Set(descriptor.Fields().Get(1), protoreflect.ValueOfString("Nebula catalog"))

	for _, test := range []struct {
		query, path string
	}{
		{"record", "payload/0"},
		{"description", "payload/0/description"},
		{"string", "payload/0/description"},
		{"orion neb", "payload/0/description"},
	} {
		m := New([]protoreflect.Message{message.ProtoReflect()}, []int{42}, "test.Record", "test", "fixture")
		m.beginSearch(test.query)
		hit, done := m.advanceSearch(100)
		if done || hit == nil || hit.path != test.path {
			t.Fatalf("query %q: hit = %#v, done = %v", test.query, hit, done)
		}
	}

	m := New([]protoreflect.Message{message.ProtoReflect()}, []int{42}, "test.Record", "test", "fixture")
	m.height = 10
	m.beginSearch("nebula")
	hit, done := m.advanceSearch(100)
	if done || hit == nil || hit.path != "payload/0/description" {
		t.Fatalf("search hit = %#v, done = %v", hit, done)
	}
	m.selectSearchHit(*hit)
	if !m.expanded["payload/0"] || m.cursor != 1 {
		t.Fatalf("search did not reveal result: expanded=%v cursor=%d", m.expanded, m.cursor)
	}
	next, done := m.advanceSearch(100)
	if done || next == nil || next.path != "payload/0/catalog" {
		t.Fatalf("find next hit = %#v, done = %v", next, done)
	}

	multi := New([]protoreflect.Message{message.ProtoReflect(), message.ProtoReflect()}, []int{7, 123}, "test.Record", "test", "fixture")
	delete(multi.expanded, payloadPath(0))
	rows := multi.rows()
	if len(rows) != 2 || rows[0].n.path != "payload/0" || rows[1].n.path != "payload/1" {
		t.Fatalf("payload roots = %#v", rows)
	}
	if !strings.Contains(rows[0].n.label, "  7 bytes") || !strings.Contains(rows[1].n.label, "123 bytes") {
		t.Fatalf("payload sizes not shown: %q, %q", rows[0].n.label, rows[1].n.label)
	}
	if strings.Index(rows[0].n.label, "7 bytes") != strings.Index(rows[1].n.label, "123 bytes")+2 {
		t.Fatalf("payload sizes are not right-aligned: %q, %q", rows[0].n.label, rows[1].n.label)
	}
}

// Kept separate to make the test's descriptor setup readable.
func protoreflectValueOfString(value string) protoreflect.Value {
	return protoreflect.ValueOfString(value)
}
